package sandbox

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
)

// buildWriteTarStream builds a tar archive that, when extracted via
// `tar -x -C /workspace`, materialises a single file at rel containing data,
// creating intermediate directories as needed. rel must already be the path
// RELATIVE to /workspace (no leading slash). Used by both DockerDriver and
// K8sDriver to keep the workspace tmpfs write semantics identical.
func buildWriteTarStream(rel string, data []byte) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

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
		return nil, fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return nil, fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("tar close: %w", err)
	}
	return buf.Bytes(), nil
}

// parseReadTarStream reads the FIRST regular-file entry from a tar stream
// produced by `tar -c -C /workspace -- <rel>`. EOF before any entry maps to
// ErrSandboxNotFound (file missing inside the sandbox); a directory entry
// returns an error; size > MaxFileSize returns ErrTooLarge.
func parseReadTarStream(tarBytes []byte) ([]byte, error) {
	tr := tar.NewReader(bytes.NewReader(tarBytes))
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

// stripWorkspacePrefix turns an absolute /workspace path into a relative path
// usable as a tar entry name. Returns "" if the input is /workspace itself
// (callers reject this as ErrPathOutsideWorkspace at the boundary).
func stripWorkspacePrefix(abs string) string {
	rel := strings.TrimPrefix(abs, workspaceRoot+"/")
	if rel == abs {
		return ""
	}
	return rel
}
