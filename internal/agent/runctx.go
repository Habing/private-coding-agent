package agent

import (
	"context"

	"github.com/google/uuid"
)

// MaxDelegateDepth caps how many times agent.delegate may be nested.
// Slice 18 fixes this at 1: the top-level Run is depth 0, a single delegated
// child Run is depth 1, and any further delegate call from depth 1 is
// rejected. Sub-profiles also do not carry agent.delegate in their tool
// allowlist, so this counter is the second of two safety nets.
const MaxDelegateDepth = 1

// RunCtx carries per-Run state that the agent package needs to surface to
// tools (notably agent.delegate). It is propagated via context so that the
// existing Tool interface — which lacks a *RunInput parameter — can still
// read parent sandbox / model / depth without callers having to thread an
// extra argument or rely on the LLM to fill it in.
//
// Engine.Run injects this at the top of each Run; agent.delegate reads it,
// bumps DelegateDepth by one, and stamps the child RunInput with the parent
// SandboxID + Model before calling Engine.Run again.
type RunCtx struct {
	SandboxID     uuid.UUID
	Model         string
	ProfileName   string
	DelegateDepth int
}

type runCtxKey struct{}

// WithRunCtx returns a context carrying the supplied RunCtx. A nested call
// fully overrides the previous value — we never merge.
func WithRunCtx(ctx context.Context, rc RunCtx) context.Context {
	return context.WithValue(ctx, runCtxKey{}, rc)
}

// RunCtxFromCtx returns the RunCtx stored in ctx, or a zero RunCtx if none
// is present. Callers should treat zero SandboxID / empty Model as "called
// outside an Engine.Run" — typically because the tool was invoked directly
// via POST /tools/invoke.
func RunCtxFromCtx(ctx context.Context) RunCtx {
	if ctx == nil {
		return RunCtx{}
	}
	if v, ok := ctx.Value(runCtxKey{}).(RunCtx); ok {
		return v
	}
	return RunCtx{}
}
