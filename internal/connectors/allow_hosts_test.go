package connectors_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/connectors"
)

func TestValidateAllowHosts(t *testing.T) {
	got, err := connectors.ValidateAllowHosts([]string{" mock-provider ", "*.baidu.com", "top.baidu.com"})
	require.NoError(t, err)
	require.Equal(t, []string{"mock-provider", "*.baidu.com", "top.baidu.com"}, got)

	_, err = connectors.ValidateAllowHosts([]string{})
	require.Error(t, err)

	_, err = connectors.ValidateAllowHosts([]string{"bad host"})
	require.Error(t, err)
}

func TestNormalizeAllowHosts(t *testing.T) {
	got := connectors.NormalizeAllowHosts([]string{" A.COM ", "a.com", ""})
	require.Equal(t, []string{"a.com"}, got)
}
