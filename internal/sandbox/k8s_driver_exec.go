package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilexec "k8s.io/client-go/util/exec"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// k8sExecer is the subset of remotecommand.Executor that K8sDriver.Exec
// drives. Real builds use a tiny adapter around remotecommand.NewSPDYExecutor;
// unit tests inject a stub via K8sDriver.execerFactory.
type k8sExecer interface {
	StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error
}

// realExecer adapts remotecommand.Executor to k8sExecer.
type realExecer struct{ exec remotecommand.Executor }

func (r *realExecer) StreamWithContext(ctx context.Context, opts remotecommand.StreamOptions) error {
	return r.exec.StreamWithContext(ctx, opts)
}

// newSPDYExecer constructs the production SPDY-backed executor. The URL is
// built from the rest config — same approach kubectl exec uses.
func newSPDYExecer(restCfg *rest.Config, ns, podName string, opts ExecOpts) (k8sExecer, error) {
	if restCfg == nil {
		return nil, errors.New("k8s exec: rest config is nil — set sandbox.k8s.in_cluster or kubeconfig")
	}
	parameterCodec := runtime.NewParameterCodec(scheme.Scheme)
	req := restClientFor(restCfg).Post().
		Resource("pods").
		Name(podName).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "sandbox",
			Command:   opts.Cmd,
			Stdin:     len(opts.Stdin) > 0,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, parameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(restCfg, "POST", req.URL())
	if err != nil {
		return nil, fmt.Errorf("new SPDY executor: %w", err)
	}
	return &realExecer{exec: executor}, nil
}

// restClientFor builds a CoreV1 REST client. Split out so tests don't drag
// in API server discovery. Returns the same client kubectl uses for exec.
func restClientFor(cfg *rest.Config) *rest.RESTClient {
	// Clone so we can mutate ContentConfig without poisoning the caller's cfg.
	c := *cfg
	c.GroupVersion = &corev1.SchemeGroupVersion
	c.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	c.APIPath = "/api"
	rc, err := rest.RESTClientFor(&c)
	if err != nil {
		// Should be unreachable when cfg was built by clientcmd / InClusterConfig.
		// Panic here rather than weaving the error through every call site —
		// the real failure mode is misconfigured boot-time auth, which surfaces
		// at first exec attempt.
		panic(fmt.Sprintf("k8s exec: rest client init: %v", err))
	}
	return rc
}

// Exec runs cmd inside the sandbox Pod via SPDY remote-exec. Streams are
// each capped at MaxStreamBytes (same as DockerDriver). On TimeoutSec the
// ctx is canceled, which terminates the SPDY stream.
func (d *K8sDriver) Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (execOut *ExecResult, execErr error) {
	ctx, span := tracer.Start(ctx, "sandbox.exec.k8s",
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

	name, err := d.requirePodName(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	executor, err := d.execerFactory(d.restCfg, d.cfg.Namespace, name, opts)
	if err != nil {
		return nil, fmt.Errorf("build executor: %w", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, time.Duration(opts.TimeoutSec)*time.Second)
	defer cancel()

	stdoutBuf := newLimitedBuffer(MaxStreamBytes)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)
	var stdin io.Reader
	if len(opts.Stdin) > 0 {
		stdin = bytes.NewReader(opts.Stdin)
	}

	start := time.Now()
	err = executor.StreamWithContext(streamCtx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdoutBuf,
		Stderr: stderrBuf,
		Tty:    false,
	})
	durationMS := time.Since(start).Milliseconds()

	timedOut := errors.Is(streamCtx.Err(), context.DeadlineExceeded)
	exitCode := 0
	if err != nil {
		// remotecommand maps a non-zero exit into utilexec.CodeExitError;
		// any other error means the channel itself failed (network, auth).
		var codeErr utilexec.CodeExitError
		if errors.As(err, &codeErr) {
			exitCode = codeErr.Code
		} else if timedOut {
			exitCode = -1
		} else {
			return nil, fmt.Errorf("exec stream: %w", err)
		}
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

// requirePodName mirrors DockerDriver.requireContainerID: enforces Running
// status + non-empty container_id (here, Pod name) before exec/file ops.
func (d *K8sDriver) requirePodName(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if sb.Status == StatusDestroyed {
		return "", ErrSandboxNotFound
	}
	if sb.Status != StatusRunning {
		return "", ErrSandboxNotReady
	}
	name, err := d.repo.GetContainerID(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", ErrSandboxNotReady
	}
	return name, nil
}
