package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestResolveWorkspacePath_OK(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo.txt", "/workspace/foo.txt"},
		{"a/b/c.txt", "/workspace/a/b/c.txt"},
		{"./foo.txt", "/workspace/foo.txt"},
		{"/workspace/foo.txt", "/workspace/foo.txt"},
		{"/workspace", "/workspace"},
		{"/workspace/a/b/c.txt", "/workspace/a/b/c.txt"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := sandbox.ResolveWorkspacePath(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestResolveWorkspacePath_Reject(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"../../etc/passwd",
		"/../etc/passwd",
		"/etc/passwd",
		"/workspace/../etc/passwd",
		"/var/log/x",
		"/foo.txt",
		"/a/b/c.txt",
		"",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := sandbox.ResolveWorkspacePath(in)
			require.ErrorIs(t, err, sandbox.ErrPathOutsideWorkspace)
		})
	}
}
