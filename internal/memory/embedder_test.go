package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/memory"
)

// fakeEmbedder is a test stub returning deterministic vectors by content.
// Each call records inputs so tests can assert the embedder was invoked.
type fakeEmbedder struct {
	dim    int
	vecs   map[string][]float32
	err    error
	calls  [][]string
	scoped bool
}

func newFakeEmbedder(dim int) *fakeEmbedder {
	return &fakeEmbedder{dim: dim, vecs: map[string][]float32{}}
}

func (f *fakeEmbedder) Dim() int { return f.dim }

func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.calls = append(f.calls, append([]string(nil), inputs...))
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		if v, ok := f.vecs[s]; ok {
			out[i] = v
			continue
		}
		// deterministic fallback: 1.0 at index hash(s) % dim, 0 elsewhere.
		vec := make([]float32, f.dim)
		h := 0
		for _, b := range []byte(s) {
			h = (h*31 + int(b)) & 0x7fffffff
		}
		vec[h%f.dim] = 1
		out[i] = vec
	}
	return out, nil
}

func (f *fakeEmbedder) preset(s string, v []float32) { f.vecs[s] = v }

func TestEmbeddingDim(t *testing.T) {
	require.Equal(t, 1536, memory.EmbeddingDim)
}

func TestFakeEmbedder_Deterministic(t *testing.T) {
	f := newFakeEmbedder(memory.EmbeddingDim)
	v1, err := f.Embed(context.Background(), []string{"hello"})
	require.NoError(t, err)
	v2, err := f.Embed(context.Background(), []string{"hello"})
	require.NoError(t, err)
	require.Equal(t, v1, v2)
	require.Len(t, v1[0], memory.EmbeddingDim)
}

func TestFakeEmbedder_PropagatesError(t *testing.T) {
	f := newFakeEmbedder(memory.EmbeddingDim)
	f.err = errors.New("boom")
	_, err := f.Embed(context.Background(), []string{"hi"})
	require.Error(t, err)
}
