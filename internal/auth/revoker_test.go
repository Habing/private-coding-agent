package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func newRevoker(t *testing.T) (*auth.RedisRevoker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return auth.NewRedisRevoker(rdb), mr
}

func TestRevoker_RevokeAndCheck(t *testing.T) {
	rev, _ := newRevoker(t)
	ctx := context.Background()

	require.NoError(t, rev.Revoke(ctx, "jti-1", time.Hour))
	got, err := rev.IsRevoked(ctx, "jti-1")
	require.NoError(t, err)
	require.True(t, got)

	got, err = rev.IsRevoked(ctx, "jti-fresh")
	require.NoError(t, err)
	require.False(t, got)
}

func TestRevoker_TTLExpires(t *testing.T) {
	rev, mr := newRevoker(t)
	ctx := context.Background()

	require.NoError(t, rev.Revoke(ctx, "jti-x", 5*time.Second))
	// miniredis fast-forward — simulates TTL expiry.
	mr.FastForward(6 * time.Second)
	got, err := rev.IsRevoked(ctx, "jti-x")
	require.NoError(t, err)
	require.False(t, got, "expired revocation should drop out")
}

func TestRevoker_EmptyJTIIsNotRevoked(t *testing.T) {
	rev, _ := newRevoker(t)
	got, err := rev.IsRevoked(context.Background(), "")
	require.NoError(t, err)
	require.False(t, got)
}

func TestRevoker_NonPositiveTTLIsNoOp(t *testing.T) {
	rev, mr := newRevoker(t)
	require.NoError(t, rev.Revoke(context.Background(), "jti-zero", 0))
	require.Empty(t, mr.Keys(), "zero TTL should not write a key")
}
