// Package quota enforces per-tenant + per-user usage caps backed by Redis
// fixed-window counters. Three Kinds map onto orthogonal counters:
//
//   KindLLMTokens     — sum of input + output tokens, UTC-day window
//   KindSandboxActive — running sandbox count for tenant (sourced from DB,
//                       so this Service only exposes a Cap getter)
//   KindToolInvoke    — tool invocations, per-minute window
//
// A cap of 0 disables that check (Service methods return ErrQuotaExceeded
// only when the configured cap > 0 and the counter strictly exceeds it).
package quota

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// ErrQuotaExceeded is the sentinel returned when a check would push the
// counter strictly above the configured cap.
var ErrQuotaExceeded = errors.New("quota exceeded")

// Kind discriminates the three counter families.
type Kind string

const (
	KindLLMTokens  Kind = "llm.tokens"
	KindToolInvoke Kind = "tool.invoke"
)

// Limits carries the per-Kind caps; 0 == disabled.
type Limits struct {
	LLMTokensPerDay     int
	SandboxMaxActive    int
	ToolInvokePerMinute int
}

// Service is the public quota surface. Two flavors:
//
//   - CheckAndIncr: atomically reserve N units against a counter. The caller
//     supplies a worst-case estimate (e.g. expected output tokens for LLM,
//     1 for tool invokes); we increment by that amount and refuse when the
//     new total would exceed the cap. The reservation is best-effort: we
//     never refund on failure because (a) the cap is generous and (b) the
//     refund path itself can fail under partition.
//   - Adjust: applied AFTER the work completes to reconcile the estimate
//     with the actual count (only useful for LLM tokens, where the
//     pre-call estimate may diverge from the recorded usage).
//   - SandboxCap: pure getter; the caller compares against a fresh DB count.
type Service struct {
	rdb    *redis.Client
	limits Limits
	now    func() time.Time
}

func NewService(rdb *redis.Client, limits Limits) *Service {
	return &Service{rdb: rdb, limits: limits, now: time.Now}
}

// SetNowForTest overrides the clock (test-only).
func (s *Service) SetNowForTest(fn func() time.Time) { s.now = fn }

// Limits returns the configured caps (read-only snapshot).
func (s *Service) Limits() Limits { return s.limits }

// CheckAndIncr atomically increments the counter for (kind, tenant, user) by
// delta and returns ErrQuotaExceeded if the resulting value would be strictly
// greater than the configured cap. delta must be > 0.
//
// kind must be KindLLMTokens or KindToolInvoke. KindSandboxActive does not
// use a counter (the caller queries the DB).
func (s *Service) CheckAndIncr(ctx context.Context, kind Kind, tenantID, userID uuid.UUID, delta int) error {
	if delta <= 0 {
		return fmt.Errorf("quota: delta must be > 0, got %d", delta)
	}
	cap, ttl, err := s.limitFor(kind)
	if err != nil {
		return err
	}
	if cap <= 0 {
		return nil
	}
	key := s.windowKey(kind, tenantID, userID)

	// Pipeline: INCRBY then EXPIRE (NX so we don't keep pushing TTL out on
	// every hit). We rely on the first INCRBY of a window seeing key=0 and
	// EXPIRE NX setting the TTL once.
	pipe := s.rdb.TxPipeline()
	incrCmd := pipe.IncrBy(ctx, key, int64(delta))
	pipe.ExpireNX(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("quota redis: %w", err)
	}
	if int(incrCmd.Val()) > cap {
		return fmt.Errorf("%w: %s tenant=%s used=%d cap=%d",
			ErrQuotaExceeded, kind, tenantID, incrCmd.Val(), cap)
	}
	return nil
}

// Adjust adds (or subtracts, when delta < 0) units to an existing window
// counter without re-checking the cap. Used for LLM token reconciliation
// AFTER the model call completes (estimate-vs-actual). Safe to call when
// the window has already expired — Redis simply recreates the key, but
// since the window has rolled over the count starts fresh, which is the
// intended semantics.
func (s *Service) Adjust(ctx context.Context, kind Kind, tenantID, userID uuid.UUID, delta int) error {
	if delta == 0 {
		return nil
	}
	if cap, _, _ := s.limitFor(kind); cap <= 0 {
		return nil
	}
	key := s.windowKey(kind, tenantID, userID)
	if err := s.rdb.IncrBy(ctx, key, int64(delta)).Err(); err != nil {
		return fmt.Errorf("quota redis adjust: %w", err)
	}
	return nil
}

// SandboxCap returns the per-tenant cap for active sandboxes (0 = unlimited).
// The caller compares this to a fresh COUNT(*) from sandbox_sessions.
func (s *Service) SandboxCap() int { return s.limits.SandboxMaxActive }

// Usage is a point-in-time counter snapshot for one Kind window.
type Usage struct {
	Used int
	Cap  int // 0 when the Kind check is disabled
}

// GetUsage reads the current window counter without incrementing it.
func (s *Service) GetUsage(ctx context.Context, kind Kind, tenantID, userID uuid.UUID) (Usage, error) {
	cap, _, err := s.limitFor(kind)
	if err != nil {
		return Usage{}, err
	}
	if cap <= 0 {
		return Usage{Cap: 0}, nil
	}
	key := s.windowKey(kind, tenantID, userID)
	val, err := s.rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return Usage{Used: 0, Cap: cap}, nil
	}
	if err != nil {
		return Usage{}, fmt.Errorf("quota redis get: %w", err)
	}
	return Usage{Used: val, Cap: cap}, nil
}

// NextWindowStartUTC returns when the current fixed window rolls over (UTC).
func (s *Service) NextWindowStartUTC(kind Kind) (time.Time, error) {
	now := s.now().UTC()
	switch kind {
	case KindLLMTokens:
		y, m, d := now.Date()
		return time.Date(y, m, d+1, 0, 0, 0, 0, time.UTC), nil
	case KindToolInvoke:
		return now.Truncate(time.Minute).Add(time.Minute), nil
	default:
		return time.Time{}, fmt.Errorf("quota: unsupported kind %q", kind)
	}
}

func (s *Service) limitFor(kind Kind) (int, time.Duration, error) {
	switch kind {
	case KindLLMTokens:
		// 48h TTL so cross-midnight overlap is visible to operators
		// inspecting Redis; the per-day window key is the date stamp.
		return s.limits.LLMTokensPerDay, 48 * time.Hour, nil
	case KindToolInvoke:
		return s.limits.ToolInvokePerMinute, 2 * time.Minute, nil
	default:
		return 0, 0, fmt.Errorf("quota: unsupported kind %q", kind)
	}
}

func (s *Service) windowKey(kind Kind, tenantID, userID uuid.UUID) string {
	now := s.now().UTC()
	switch kind {
	case KindLLMTokens:
		return fmt.Sprintf("q:llm:%s:%s:%s",
			tenantID, userID, now.Format("2006-01-02"))
	case KindToolInvoke:
		return fmt.Sprintf("q:tool:%s:%s:%s",
			tenantID, userID, now.Format("2006-01-02T15:04"))
	default:
		return fmt.Sprintf("q:unknown:%s:%s", tenantID, userID)
	}
}
