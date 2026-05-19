package sandbox

import (
	"path"
	"strings"
)

const workspaceRoot = "/workspace"

// ResolveWorkspacePath takes a user-supplied path (relative or absolute) and
// returns its absolute canonical form rooted at /workspace. Rejects any path
// that, after cleaning, escapes /workspace.
//
// Symbolic link resolution is NOT performed here; the docker tar IO layer
// is configured to not follow symlinks across the boundary.
func ResolveWorkspacePath(p string) (string, error) {
	if p == "" {
		return "", ErrPathOutsideWorkspace
	}
	// 规范化: 处理 .. 和 ./
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = path.Clean(p)
	} else {
		abs = path.Clean(workspaceRoot + "/" + p)
	}
	// 必须 == /workspace 或以 /workspace/ 开头
	if abs == workspaceRoot {
		return workspaceRoot, nil
	}
	if !strings.HasPrefix(abs, workspaceRoot+"/") {
		return "", ErrPathOutsideWorkspace
	}
	return abs, nil
}
