package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func TestValidateJWTConfig_Valid(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "a-very-long-and-random-secret-1234567890",
		TTL:    time.Hour,
	})
	require.NoError(t, err)
}

func TestValidateJWTConfig_Empty(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{Secret: "", TTL: time.Hour})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "empty"))
}

func TestValidateJWTConfig_DefaultPlaceholder(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "change-me-in-production",
		TTL:    time.Hour,
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "default"))
}

func TestValidateJWTConfig_TooShort(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{Secret: "shortie", TTL: time.Hour})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "too short"))
}

func TestValidateJWTConfig_ZeroTTL(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "a-very-long-and-random-secret-1234567890",
		TTL:    0,
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "ttl"))
}
