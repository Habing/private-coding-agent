package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// EmbeddingDim is the fixed vector width assumed by every code path in this
// package. The schema column is `vector(1536)`; any embedder returning a
// different length is rejected at runtime.
const EmbeddingDim = 1536

// ErrEmbedDimMismatch is returned when an embedder produces a vector of the
// wrong length. It is fatal for the calling operation — Service refuses to
// insert mismatched vectors rather than silently storing rows invisible to
// vector search.
var ErrEmbedDimMismatch = errors.New("embedding dimension mismatch")

// Embedder turns text into fixed-dimension float32 vectors. Implementations
// must be safe for concurrent use; production wraps the model gateway, tests
// inject deterministic stubs.
type Embedder interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	Dim() int
}

// GatewayEmbedder is the production Embedder, dispatching through the model
// gateway with a configured `provider:model` string. Tenant + user are
// resolved per-call from the request context via auth.FromCtx, so a single
// instance is shared across all users.
type GatewayEmbedder struct {
	gw    *modelgw.Gateway
	model string
}

// NewGatewayEmbedder builds a production embedder. The model string follows
// the gateway's `provider:model` format.
func NewGatewayEmbedder(gw *modelgw.Gateway, model string) *GatewayEmbedder {
	return &GatewayEmbedder{gw: gw, model: model}
}

func (g *GatewayEmbedder) Dim() int { return EmbeddingDim }

func (g *GatewayEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	cl := auth.FromCtx(ctx)
	if cl == nil {
		return nil, fmt.Errorf("embed: missing auth context")
	}
	dim := EmbeddingDim
	resp, err := g.gw.Embeddings(ctx, cl.TenantID, cl.UserID, modelgw.EmbeddingsRequest{
		Model:      g.model,
		Input:      inputs,
		Dimensions: &dim,
	})
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	if len(resp.Data) != len(inputs) {
		return nil, fmt.Errorf("embed: provider returned %d vectors for %d inputs",
			len(resp.Data), len(inputs))
	}
	out := make([][]float32, len(resp.Data))
	for i, d := range resp.Data {
		if len(d.Embedding) != EmbeddingDim {
			return nil, fmt.Errorf("%w: got %d want %d", ErrEmbedDimMismatch,
				len(d.Embedding), EmbeddingDim)
		}
		vec := make([]float32, EmbeddingDim)
		for j, v := range d.Embedding {
			vec[j] = float32(v)
		}
		out[i] = vec
	}
	return out, nil
}
