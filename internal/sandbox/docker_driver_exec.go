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
)

// Exec runs cmd inside the sandbox synchronously.
func (d *DockerDriver) Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (*ExecResult, error) {
	opts, err := NormalizeExecOpts(opts)
	if err != nil {
		return nil, err
	}

	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
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

	if len(opts.Stdin) > 0 {
		_, _ = attached.Conn.Write(opts.Stdin)
		_ = attached.CloseWrite()
	}

	stdoutBuf := newLimitedBuffer(MaxStreamBytes)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)

	start := time.Now()
	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader)
		copyErr <- err
	}()

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
	if err == nil {
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
