package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Revoker stores and checks revoked JWT IDs (jti claim). A Redis-backed
// implementation is provided; production wires this into the middleware via
// WithRevoker. Tests can pass an in-memory fake.
//
// The TTL of a revocation record matches the token's remaining lifetime, so
// the store doesn't grow unboundedly: expired tokens are useless anyway, so
// holding revocation entries past expiry wastes memory.
type Revoker interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
	Revoke(ctx context.Context, jti string, ttl time.Duration) error
}

// RedisRevoker is a Revoker backed by go-redis. Keys live under "auth:rev:".
type RedisRevoker struct {
	rdb *redis.Client
}

// NewRedisRevoker constructs a RedisRevoker. nil rdb is supported only at
// construction; method calls on a zero RedisRevoker will fail.
func NewRedisRevoker(rdb *redis.Client) *RedisRevoker {
	return &RedisRevoker{rdb: rdb}
}

// IsRevoked returns true if jti is currently in the revoked set. Empty jti
// is never revoked (legacy tokens lack the claim — treat as live until
// natural expiry).
func (r *RedisRevoker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	n, err := r.rdb.Exists(ctx, revokedKey(jti)).Result()
	if err != nil {
		return false, fmt.Errorf("auth revoker: %w", err)
	}
	return n > 0, nil
}

// Revoke records jti as revoked for ttl. A non-positive ttl is a no-op: the
// token has already expired and revocation is moot. Empty jti is ignored
// (defensive — caller should have filtered this earlier).
func (r *RedisRevoker) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if jti == "" || ttl <= 0 {
		return nil
	}
	if err := r.rdb.Set(ctx, revokedKey(jti), 1, ttl).Err(); err != nil {
		return fmt.Errorf("auth revoker: %w", err)
	}
	return nil
}

func revokedKey(jti string) string { return "auth:rev:" + jti }
