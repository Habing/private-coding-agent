package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestStatusConstants(t *testing.T) {
	require.Equal(t, "pending", string(sandbox.StatusPending))
	require.Equal(t, "running", string(sandbox.StatusRunning))
	require.Equal(t, "destroying", string(sandbox.StatusDestroying))
	require.Equal(t, "destroyed", string(sandbox.StatusDestroyed))
	require.Equal(t, "failed", string(sandbox.StatusFailed))
}

func TestNetworkModeConstants(t *testing.T) {
	require.Equal(t, "internal", string(sandbox.NetworkInternal))
	require.Equal(t, "bridge", string(sandbox.NetworkBridge))
	require.Equal(t, "none", string(sandbox.NetworkNone))
}

func TestDefaults(t *testing.T) {
	require.Equal(t, "pca/sandbox:base", sandbox.DefaultImage)
	require.Equal(t, sandbox.NetworkInternal, sandbox.DefaultNetwork)
	require.Equal(t, 1.0, sandbox.DefaultCPUs)
	require.Equal(t, int64(512), sandbox.DefaultMemoryMB)
	require.Equal(t, int64(256), sandbox.DefaultPIDsLimit)
	require.Equal(t, 60, sandbox.DefaultExecTimeoutSec)
}

func TestUpperLimits(t *testing.T) {
	require.Equal(t, 4.0, sandbox.MaxCPUs)
	require.Equal(t, int64(4096), sandbox.MaxMemoryMB)
	require.Equal(t, int64(1024), sandbox.MaxPIDsLimit)
	require.Equal(t, 600, sandbox.MaxExecTimeoutSec)
	require.Equal(t, 1<<20, sandbox.MaxFileSize)         // 1 MB
	require.Equal(t, 128*1024, sandbox.MaxStreamBytes)    // 128 KB per stream
}

func TestErrors(t *testing.T) {
	require.Error(t, sandbox.ErrSandboxNotFound)
	require.Error(t, sandbox.ErrSandboxNotReady)
	require.Error(t, sandbox.ErrPathOutsideWorkspace)
	require.Error(t, sandbox.ErrNotImplemented)
	require.Error(t, sandbox.ErrTooLarge)
}
