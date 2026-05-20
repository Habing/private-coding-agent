package modelgw_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestRoleConstants(t *testing.T) {
	require.Equal(t, "system", string(modelgw.RoleSystem))
	require.Equal(t, "user", string(modelgw.RoleUser))
	require.Equal(t, "assistant", string(modelgw.RoleAssistant))
	require.Equal(t, "tool", string(modelgw.RoleTool))
}

func TestLimitConstants(t *testing.T) {
	require.Equal(t, 200, modelgw.MaxMessages)
	require.Equal(t, 256*1024, modelgw.MaxMessageBytes)
	require.Equal(t, 100, modelgw.MaxEmbeddingInput)
	require.Equal(t, 8*1024, modelgw.MaxEmbeddingItem)
	require.Equal(t, 120, modelgw.DefaultTimeoutSec)
}

func TestErrorSentinels(t *testing.T) {
	require.Error(t, modelgw.ErrModelInvalid)
	require.Error(t, modelgw.ErrProviderNotFound)
	require.Error(t, modelgw.ErrProviderUnreachable)
	require.Error(t, modelgw.ErrProviderError)
	require.Error(t, modelgw.ErrUnsupportedFeature)
}

func TestProviderErrorIs(t *testing.T) {
	pe := &modelgw.ProviderError{StatusCode: 503, Body: "x"}
	require.True(t, errors.Is(pe, modelgw.ErrProviderError))
	require.Equal(t, "provider 503: x", pe.Error())
}
