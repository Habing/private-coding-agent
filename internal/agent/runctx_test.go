package agent_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestRunCtxFromCtx_AbsentReturnsZero(t *testing.T) {
	rc := agent.RunCtxFromCtx(context.Background())
	require.Equal(t, uuid.Nil, rc.SandboxID)
	require.Equal(t, "", rc.Model)
	require.Equal(t, "", rc.ProfileName)
	require.Equal(t, 0, rc.DelegateDepth)
}

func TestRunCtxFromCtx_NilCtxReturnsZero(t *testing.T) {
	//nolint:staticcheck // nil-ctx defence is the point of this test
	rc := agent.RunCtxFromCtx(nil)
	require.Equal(t, agent.RunCtx{}, rc)
}

func TestWithRunCtx_RoundTrip(t *testing.T) {
	sb := uuid.New()
	rc := agent.RunCtx{
		SandboxID:     sb,
		Model:         "default-mock:gpt-4o",
		ProfileName:   "coding",
		DelegateDepth: 1,
	}
	ctx := agent.WithRunCtx(context.Background(), rc)

	got := agent.RunCtxFromCtx(ctx)
	require.Equal(t, sb, got.SandboxID)
	require.Equal(t, "default-mock:gpt-4o", got.Model)
	require.Equal(t, "coding", got.ProfileName)
	require.Equal(t, 1, got.DelegateDepth)
}

func TestWithRunCtx_NestedOverrides(t *testing.T) {
	first := agent.RunCtx{SandboxID: uuid.New(), DelegateDepth: 0}
	second := agent.RunCtx{SandboxID: uuid.New(), DelegateDepth: 1}

	ctx := agent.WithRunCtx(context.Background(), first)
	ctx = agent.WithRunCtx(ctx, second)

	got := agent.RunCtxFromCtx(ctx)
	require.Equal(t, second.SandboxID, got.SandboxID)
	require.Equal(t, 1, got.DelegateDepth)
}

func TestMaxDelegateDepth_IsOne(t *testing.T) {
	// Sanity check — Slice 18's whole safety story relies on this.
	require.Equal(t, 1, agent.MaxDelegateDepth)
}
