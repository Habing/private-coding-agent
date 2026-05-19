package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func TestIssueAndParse(t *testing.T) {
	svc := auth.NewJWT(auth.JWTConfig{Secret: "test-secret", TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := svc.Issue(uid, tid, "member")
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	c, err := svc.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, uid, c.UserID)
	require.Equal(t, tid, c.TenantID)
	require.Equal(t, "member", c.Role)
}

func TestParse_Expired(t *testing.T) {
	svc := auth.NewJWT(auth.JWTConfig{Secret: "test-secret", TTL: -time.Minute})
	tok, err := svc.Issue(uuid.New(), uuid.New(), "member")
	require.NoError(t, err)
	_, err = svc.Parse(tok)
	require.Error(t, err)
}

func TestParse_BadSecret(t *testing.T) {
	a := auth.NewJWT(auth.JWTConfig{Secret: "k1", TTL: time.Hour})
	b := auth.NewJWT(auth.JWTConfig{Secret: "k2", TTL: time.Hour})
	tok, _ := a.Issue(uuid.New(), uuid.New(), "member")
	_, err := b.Parse(tok)
	require.Error(t, err)
}
