package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

const snapshotWorkspaceKeySuffix = ".workspace.tar"

func workspaceSnapshotKey(rootKey string) string {
	if strings.HasSuffix(rootKey, ".tar") {
		return strings.TrimSuffix(rootKey, ".tar") + snapshotWorkspaceKeySuffix
	}
	return rootKey + snapshotWorkspaceKeySuffix
}

// streamWorkspaceTarFromContainer execs `tar -c -C /workspace .` and returns
// stdout as a tar stream. Tmpfs /workspace is omitted from docker export/commit,
// so workspace content is captured separately.
func streamWorkspaceTarFromContainer(ctx context.Context, cli *client.Client, cid string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		var copyErr error
		defer func() {
			if copyErr != nil {
				_ = pw.CloseWithError(copyErr)
				return
			}
			_ = pw.Close()
		}()

		execCfg := container.ExecOptions{
			Cmd:          []string{"tar", "-c", "-C", workspaceRoot, "--", "."},
			AttachStdout: true,
			AttachStderr: true,
		}
		created, err := cli.ContainerExecCreate(ctx, cid, execCfg)
		if err != nil {
			copyErr = fmt.Errorf("workspace tar exec create: %w", err)
			return
		}

		attachCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		attached, err := cli.ContainerExecAttach(attachCtx, created.ID, container.ExecAttachOptions{})
		if err != nil {
			copyErr = fmt.Errorf("workspace tar exec attach: %w", err)
			return
		}
		defer attached.Close()

		stderrBuf := newLimitedBuffer(MaxStreamBytes)
		if _, err := stdcopy.StdCopy(pw, stderrBuf, attached.Reader); err != nil && err != io.EOF {
			copyErr = fmt.Errorf("workspace tar stdcopy: %w", err)
			return
		}

		inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		insp, err := cli.ContainerExecInspect(inspectCtx, created.ID)
		if err != nil {
			copyErr = fmt.Errorf("workspace tar exec inspect: %w", err)
			return
		}
		if insp.ExitCode != 0 {
			copyErr = fmt.Errorf("workspace tar exec: exit=%d stderr=%q", insp.ExitCode, string(stderrBuf.Bytes()))
		}
	}()
	return pr, nil
}

func restoreWorkspaceTarToContainer(ctx context.Context, cli *client.Client, cid string, r io.Reader) error {
	execCfg := container.ExecOptions{
		Cmd:          []string{"tar", "-x", "-C", workspaceRoot},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}
	created, err := cli.ContainerExecCreate(ctx, cid, execCfg)
	if err != nil {
		return fmt.Errorf("workspace restore exec create: %w", err)
	}

	attachCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	attached, err := cli.ContainerExecAttach(attachCtx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("workspace restore exec attach: %w", err)
	}
	defer attached.Close()

	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(io.Discard, io.Discard, attached.Reader)
		copyErr <- err
	}()

	go func() {
		_, _ = io.Copy(attached.Conn, r)
		_ = attached.CloseWrite()
	}()

	select {
	case <-attachCtx.Done():
		_ = cli.ContainerKill(context.Background(), cid, "SIGKILL")
		<-copyErr
		return attachCtx.Err()
	case err := <-copyErr:
		if err != nil && err != io.EOF {
			return fmt.Errorf("workspace restore stdcopy: %w", err)
		}
	}

	inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	insp, err := cli.ContainerExecInspect(inspectCtx, created.ID)
	if err != nil {
		return fmt.Errorf("workspace restore exec inspect: %w", err)
	}
	if insp.ExitCode != 0 {
		return fmt.Errorf("workspace restore exec: exit=%d", insp.ExitCode)
	}
	return nil
}
