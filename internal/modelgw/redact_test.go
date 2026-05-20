package modelgw

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedact_ReplacesEnvValue(t *testing.T) {
	t.Setenv("TEST_REDACT_KEY", "sk-secret-1234567890abcdef")
	out := redact("error: bad key sk-secret-1234567890abcdef seen", []string{"TEST_REDACT_KEY"})
	require.Equal(t, "error: bad key [REDACTED] seen", out)
}

func TestRedact_NoEnvNoChange(t *testing.T) {
	out := redact("plain body", []string{"TEST_REDACT_KEY_UNSET"})
	require.Equal(t, "plain body", out)
}

func TestRedact_EmptyEnvSkipped(t *testing.T) {
	t.Setenv("TEST_REDACT_KEY", "")
	out := redact("plain body", []string{"TEST_REDACT_KEY"})
	require.Equal(t, "plain body", out)
}

func TestRedact_ShortValueSkipped(t *testing.T) {
	t.Setenv("TEST_SHORT", "short")
	out := redact("contains short here", []string{"TEST_SHORT"})
	require.True(t, strings.Contains(out, "short"))
}

func TestRedact_MultipleEnvs(t *testing.T) {
	t.Setenv("TEST_A", "alpha-secret-12345")
	t.Setenv("TEST_B", "beta-secret-12345")
	out := redact("alpha-secret-12345 and beta-secret-12345", []string{"TEST_A", "TEST_B"})
	require.Equal(t, "[REDACTED] and [REDACTED]", out)
}
