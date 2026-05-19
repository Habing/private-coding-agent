package auth_test

import (
	"encoding/base64"
	"fmt"
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

func TestParse_RejectsAlgNone(t *testing.T) {
	// 构造一个 alg=none 的伪造 token（jwt v5 不允许直接用 SignedString 签 None；自行拼）
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	now := time.Now().Add(time.Hour).Unix()
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"uid":"00000000-0000-0000-0000-000000000001","tid":"00000000-0000-0000-0000-000000000002","role":"member","exp":%d}`, now)))
	tok := header + "." + payload + "."

	svc := auth.NewJWT(auth.JWTConfig{Secret: "s", TTL: time.Hour})
	_, err := svc.Parse(tok)
	require.Error(t, err)
}
