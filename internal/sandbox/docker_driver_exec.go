package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/private-coding-agent/internal/logx"
)

// Exec runs cmd inside the sandbox synchronously.
func (d *DockerDriver) Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (execOut *ExecResult, execErr error) {
	ctx, span := tracer.Start(ctx, "sandbox.exec",
		trace.WithAttributes(
			attribute.String("sandbox.id", id.String()),
			attribute.Int("sandbox.exec.cmd_len", len(opts.Cmd)),
		))
	defer func() {
		if execErr != nil {
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
		} else if execOut != nil {
			span.SetAttributes(
				attribute.Int("sandbox.exec.exit_code", execOut.ExitCode),
				attribute.Bool("sandbox.exec.timed_out", execOut.TimedOut),
				attribute.Int64("sandbox.exec.duration_ms", execOut.DurationMS),
			)
		}
		span.End()
	}()

	opts, err := NormalizeExecOpts(opts)
	if err != nil {
		return nil, err
	}

	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	// destroyed sandboxes are reported as not found to match the "no
	// distinction exposed" contract on Get.
	if sb.Status == StatusDestroyed {
		return nil, ErrSandboxNotFound
	}
	if sb.Status != StatusRunning {
		return nil, ErrSandboxNotReady
	}
	cid, err := d.repo.GetContainerID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if cid == "" {
		return nil, ErrSandboxNotReady
	}

	execCfg := container.ExecOptions{
		Cmd:          opts.Cmd,
		WorkingDir:   opts.WorkingDir,
		Env:          envToSlice(opts.Env),
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  len(opts.Stdin) > 0,
	}
	created, err := d.cli.ContainerExecCreate(ctx, cid, execCfg)
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	attachCtx, cancel := context.WithTimeout(ctx, time.Duration(opts.TimeoutSec)*time.Second)
	defer cancel()

	attached, err := d.cli.ContainerExecAttach(attachCtx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer attached.Close()

	stdoutBuf := newLimitedBuffer(MaxStreamBytes)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)

	start := time.Now()
	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader)
		copyErr <- err
	}()

	// 必须先启 stdcopy 才写 stdin: 否则 stdin > 64KB 时, daemon 反向缓冲被
	// stderr/stdout 填满会让 Write 阻塞死锁。
	if len(opts.Stdin) > 0 {
		go func() {
			_, _ = attached.Conn.Write(opts.Stdin)
			_ = attached.CloseWrite()
		}()
	}

	timedOut := false
	select {
	case <-attachCtx.Done():
		if errors.Is(attachCtx.Err(), context.DeadlineExceeded) {
			timedOut = true
		}
		_ = d.cli.ContainerKill(context.Background(), cid, "SIGKILL")
		<-copyErr
	case <-copyErr:
	}

	durationMS := time.Since(start).Milliseconds()

	inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	insp, err := d.cli.ContainerExecInspect(inspectCtx, created.ID)
	exitCode := -1
	if err != nil {
		logx.FromCtx(ctx).Error("sandbox exec: inspect",
			"exec_id", created.ID, "err", err.Error())
	} else {
		exitCode = insp.ExitCode
	}

	return &ExecResult{
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.Bytes(),
		Stderr:     stderrBuf.Bytes(),
		Truncated:  stdoutBuf.truncated || stderrBuf.truncated,
		DurationMS: durationMS,
		TimedOut:   timedOut,
	}, nil
}

// limitedBuffer 实现按 cap 截断的 io.Writer。
type limitedBuffer struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func newLimitedBuffer(cap int) *limitedBuffer {
	return &limitedBuffer{buf: &bytes.Buffer{}, cap: cap}
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remaining := l.cap - l.buf.Len()
	if remaining <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		l.buf.Write(p[:remaining])
		l.truncated = true
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) Bytes() []byte { return l.buf.Bytes() }

var _ io.Writer = (*limitedBuffer)(nil)
