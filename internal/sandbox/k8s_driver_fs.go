package sandbox

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"k8s.io/client-go/tools/remotecommand"
)

// WriteFile writes data to /workspace/<relPath> in the sandbox Pod. The
// implementation mirrors DockerDriver: pipe a tar archive over `tar -x` on
// stdin, executed via the K8s SPDY exec channel. /workspace is an
// emptyDir{Memory} mount on the Pod, so K8s itself has no way to read or
// write that namespace from outside — tar-via-exec is the only option.
func (d *K8sDriver) WriteFile(ctx context.Context, tenantID, id uuid.UUID, relPath string, data []byte) error {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return err
	}
	if len(data) > MaxFileSize {
		return ErrTooLarge
	}
	rel := stripWorkspacePrefix(abs)
	if rel == "" {
		return ErrPathOutsideWorkspace
	}

	tarBytes, err := buildWriteTarStream(rel, data)
	if err != nil {
		return err
	}

	res, err := d.Exec(ctx, tenantID, id, ExecOpts{
		Cmd:        []string{"tar", "-x", "-C", workspaceRoot},
		Stdin:      tarBytes,
		TimeoutSec: 30,
	})
	if err != nil {
		return fmt.Errorf("exec tar: %w", err)
	}
	if res.TimedOut {
		return fmt.Errorf("tar extract timed out")
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("tar extract: exit=%d stderr=%q", res.ExitCode, string(res.Stderr))
	}
	return nil
}

// ReadFile reads /workspace/<relPath> from the sandbox Pod. Same tar-pipe
// approach as WriteFile, in the opposite direction. The MaxFileSize cap is
// enforced by parseReadTarStream.
//
// Unlike DockerDriver.ReadFile which attached directly to the exec channel
// for a larger stdout buffer, here we leverage Exec's existing buffer
// (MaxStreamBytes) — which is < MaxFileSize. To preserve parity with
// DockerDriver semantics (read up to MaxFileSize = 1 MB), we cannot route
// through d.Exec. Instead we open a dedicated executor with a bigger
// stdout buffer.
func (d *K8sDriver) ReadFile(ctx context.Context, tenantID, id uuid.UUID, relPath string) ([]byte, error) {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	rel := stripWorkspacePrefix(abs)
	if rel == "" {
		return nil, ErrPathOutsideWorkspace
	}
	name, err := d.requirePodName(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	opts, err := NormalizeExecOpts(ExecOpts{
		Cmd:        []string{"tar", "-c", "-C", workspaceRoot, "--", rel},
		TimeoutSec: 30,
	})
	if err != nil {
		return nil, err
	}

	executor, err := d.execerFactory(d.restCfg, d.cfg.Namespace, name, opts)
	if err != nil {
		return nil, fmt.Errorf("build executor: %w", err)
	}

	stdoutBuf := newLimitedBuffer(MaxFileSize + readFileBufHeadroom)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)

	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: stdoutBuf,
		Stderr: stderrBuf,
		Tty:    false,
	}); err != nil {
		// tar exits non-zero on missing entries; remotecommand surfaces that
		// as a CodeExitError. Distinguish "file missing" via stderr scan,
		// matching DockerDriver.
		if strings.Contains(string(stderrBuf.Bytes()), "No such file") ||
			strings.Contains(string(stderrBuf.Bytes()), "Cannot stat") {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("tar create: %w stderr=%q", err, string(stderrBuf.Bytes()))
	}

	return parseReadTarStream(stdoutBuf.Bytes())
}
