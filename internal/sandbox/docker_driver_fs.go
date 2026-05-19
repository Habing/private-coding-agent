package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

// readFileBufHeadroom 是 ReadFile stdout 缓冲在 MaxFileSize 之上预留的余量:
// 容纳 tar header (512 bytes) + padding (最多 511 bytes) + 一点其它噪声。
const readFileBufHeadroom = 4096

// WriteFile writes data to /workspace/<relPath> in the sandbox.
//
// Implementation note: We can't use Docker's CopyToContainer here for two
// reasons. (1) The daemon refuses it on containers with ReadonlyRootfs=true
// with "container rootfs is marked read-only" — the check happens before
// mount resolution. (2) Even if (1) were lifted, /workspace is a tmpfs mount
// that lives in the container's mount namespace; Docker's archive APIs walk
// the rootfs from outside and never see the tmpfs content. So we instead
// exec `tar -x -C /workspace` inside the container and pipe the tar stream
// over stdin; tar runs as the sandbox user against the tmpfs.
func (d *DockerDriver) WriteFile(ctx context.Context, tenantID, id uuid.UUID, relPath string, data []byte) error {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return err
	}
	if len(data) > MaxFileSize {
		return ErrTooLarge
	}

	rel := strings.TrimPrefix(abs, workspaceRoot+"/")
	if rel == "" || rel == abs {
		// abs == workspaceRoot itself — refuse writing to the workspace root.
		return ErrPathOutsideWorkspace
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Emit intermediate directory entries so tar -x will create them.
	if dir := path.Dir(rel); dir != "." && dir != "/" {
		parts := strings.Split(dir, "/")
		acc := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			if acc == "" {
				acc = p
			} else {
				acc = acc + "/" + p
			}
			_ = tw.WriteHeader(&tar.Header{
				Name:     acc + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			})
		}
	}

	if err := tw.WriteHeader(&tar.Header{
		Name: rel,
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar close: %w", err)
	}

	res, err := d.Exec(ctx, tenantID, id, ExecOpts{
		Cmd:        []string{"tar", "-x", "-C", workspaceRoot},
		Stdin:      buf.Bytes(),
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

// ReadFile reads /workspace/<relPath> from the sandbox.
//
// Implementation mirrors WriteFile's reasoning: tmpfs content is invisible to
// Docker's CopyFromContainer, so we exec `tar -c -C /workspace <rel>` inside
// the container and parse the tar stream on stdout. Because the cap we need
// here (≈ MaxFileSize) is larger than the generic Exec stream cap, we attach
// directly instead of going through d.Exec.
func (d *DockerDriver) ReadFile(ctx context.Context, tenantID, id uuid.UUID, relPath string) ([]byte, error) {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	rel := strings.TrimPrefix(abs, workspaceRoot+"/")
	if rel == "" || rel == abs {
		return nil, ErrPathOutsideWorkspace
	}
	cid, err := d.requireContainerID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	execCfg := container.ExecOptions{
		Cmd:          []string{"tar", "-c", "-C", workspaceRoot, "--", rel},
		AttachStdout: true,
		AttachStderr: true,
	}
	created, err := d.cli.ContainerExecCreate(ctx, cid, execCfg)
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	attachCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	attached, err := d.cli.ContainerExecAttach(attachCtx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer attached.Close()

	stdoutBuf := newLimitedBuffer(MaxFileSize + readFileBufHeadroom)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)
	if _, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader); err != nil && err != io.EOF {
		return nil, fmt.Errorf("stdcopy: %w", err)
	}

	inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	insp, err := d.cli.ContainerExecInspect(inspectCtx, created.ID)
	if err != nil {
		return nil, fmt.Errorf("exec inspect: %w", err)
	}
	if insp.ExitCode != 0 {
		// tar 在文件不存在时退出非零并往 stderr 写 "No such file..."。
		// 抽取为 ErrSandboxNotFound 复用现有哨兵 (调用方语义: 沙箱内某资源不存在)。
		if strings.Contains(string(stderrBuf.Bytes()), "No such file") ||
			strings.Contains(string(stderrBuf.Bytes()), "Cannot stat") {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("tar create: exit=%d stderr=%q", insp.ExitCode, string(stderrBuf.Bytes()))
	}

	tr := tar.NewReader(bytes.NewReader(stdoutBuf.Bytes()))
	hdr, err := tr.Next()
	if err == io.EOF {
		return nil, ErrSandboxNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tar next: %w", err)
	}
	if hdr.Typeflag == tar.TypeDir {
		return nil, fmt.Errorf("path_is_directory")
	}
	if hdr.Size > int64(MaxFileSize) {
		return nil, ErrTooLarge
	}

	limit := int64(MaxFileSize) + 1
	data, err := io.ReadAll(io.LimitReader(tr, limit))
	if err != nil {
		return nil, fmt.Errorf("read tar entry: %w", err)
	}
	if int64(len(data)) > int64(MaxFileSize) {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (d *DockerDriver) requireContainerID(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if sb.Status != StatusRunning {
		return "", ErrSandboxNotReady
	}
	cid, err := d.repo.GetContainerID(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if cid == "" {
		return "", ErrSandboxNotReady
	}
	return cid, nil
}
