package modelgw_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestEmbeddingsRequest_JSONDimensions(t *testing.T) {
	dim := 1536
	b, err := json.Marshal(modelgw.EmbeddingsRequest{
		Model:      "text-embedding-v4",
		Input:      []string{"x"},
		Dimensions: &dim,
	})
	require.NoError(t, err)
	require.Contains(t, string(b), `"dimensions":1536`)
}
