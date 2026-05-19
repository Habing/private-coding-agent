package sandbox_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestNormalizeCreateOpts_AppliesDefaults(t *testing.T) {
	opts := sandbox.CreateOpts{
		TenantID:    uuid.New(),
		OwnerUserID: uuid.New(),
	}
	got, err := sandbox.NormalizeCreateOpts(opts)
	require.NoError(t, err)
	require.Equal(t, sandbox.DefaultImage, got.Image)
	require.Equal(t, sandbox.DefaultNetwork, got.Network)
	require.Equal(t, sandbox.DefaultCPUs, got.Resources.CPUs)
	require.Equal(t, sandbox.DefaultMemoryMB, got.Resources.MemoryMB)
	require.Equal(t, sandbox.DefaultPIDsLimit, got.Resources.PIDsLimit)
}

func TestNormalizeCreateOpts_PreservesValid(t *testing.T) {
	opts := sandbox.CreateOpts{
		TenantID:    uuid.New(),
		OwnerUserID: uuid.New(),
		Image:       "custom:tag",
		Network:     sandbox.NetworkBridge,
		Resources:   sandbox.ResourceLimits{CPUs: 2, MemoryMB: 1024, PIDsLimit: 512},
	}
	got, err := sandbox.NormalizeCreateOpts(opts)
	require.NoError(t, err)
	require.Equal(t, "custom:tag", got.Image)
	require.Equal(t, sandbox.NetworkBridge, got.Network)
	require.Equal(t, 2.0, got.Resources.CPUs)
}

func TestNormalizeCreateOpts_RejectsBadInputs(t *testing.T) {
	base := sandbox.CreateOpts{TenantID: uuid.New(), OwnerUserID: uuid.New()}
	cases := []struct {
		name string
		mod  func(o *sandbox.CreateOpts)
	}{
		{"zero TenantID", func(o *sandbox.CreateOpts) { o.TenantID = uuid.Nil }},
		{"zero OwnerUserID", func(o *sandbox.CreateOpts) { o.OwnerUserID = uuid.Nil }},
		{"CPUs over max", func(o *sandbox.CreateOpts) { o.Resources.CPUs = sandbox.MaxCPUs + 1 }},
		{"MemoryMB over max", func(o *sandbox.CreateOpts) { o.Resources.MemoryMB = sandbox.MaxMemoryMB + 1 }},
		{"PIDsLimit over max", func(o *sandbox.CreateOpts) { o.Resources.PIDsLimit = sandbox.MaxPIDsLimit + 1 }},
		{"unknown NetworkMode", func(o *sandbox.CreateOpts) { o.Network = "weird" }},
		{"negative CPUs", func(o *sandbox.CreateOpts) { o.Resources.CPUs = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := base
			c.mod(&o)
			_, err := sandbox.NormalizeCreateOpts(o)
			require.Error(t, err)
		})
	}
}

func TestNormalizeExecOpts_AppliesDefaults(t *testing.T) {
	o, err := sandbox.NormalizeExecOpts(sandbox.ExecOpts{Cmd: []string{"echo", "hi"}})
	require.NoError(t, err)
	require.Equal(t, "/workspace", o.WorkingDir)
	require.Equal(t, sandbox.DefaultExecTimeoutSec, o.TimeoutSec)
}

func TestNormalizeExecOpts_Rejects(t *testing.T) {
	cases := []struct {
		name string
		o    sandbox.ExecOpts
	}{
		{"empty cmd", sandbox.ExecOpts{}},
		{"cmd with empty string", sandbox.ExecOpts{Cmd: []string{""}}},
		{"timeout over max", sandbox.ExecOpts{Cmd: []string{"x"}, TimeoutSec: sandbox.MaxExecTimeoutSec + 1}},
		{"negative timeout", sandbox.ExecOpts{Cmd: []string{"x"}, TimeoutSec: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := sandbox.NormalizeExecOpts(c.o)
			require.Error(t, err)
		})
	}
}
