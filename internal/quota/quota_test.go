package quota_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/quota"
)

func newSvc(t *testing.T, limits quota.Limits) (*quota.Service, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return quota.NewService(rdb, limits), mr
}

func TestCheckAndIncr_UnderCap(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{ToolInvokePerMinute: 5})
	ctx := context.Background()
	tid, uid := uuid.New(), uuid.New()
	for i := 1; i <= 5; i++ {
		require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid, uid, 1),
			"call %d should be allowed", i)
	}
	// 6th exceeds the cap.
	err := svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid, uid, 1)
	require.ErrorIs(t, err, quota.ErrQuotaExceeded)
}

func TestCheckAndIncr_CapZeroDisables(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{ToolInvokePerMinute: 0})
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, uuid.New(), uuid.New(), 1))
	}
}

func TestCheckAndIncr_LLMTokensBigDelta(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{LLMTokensPerDay: 1000})
	ctx := context.Background()
	tid, uid := uuid.New(), uuid.New()
	// First call reserves 800 — under cap.
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, uid, 800))
	// Second call reserves 300 — pushes to 1100 > cap → reject.
	err := svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, uid, 300)
	require.ErrorIs(t, err, quota.ErrQuotaExceeded)
	// A user-scope counter for a different user is independent.
	otherUser := uuid.New()
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, otherUser, 900))
}

func TestCheckAndIncr_Isolation_TenantUser(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{ToolInvokePerMinute: 2})
	ctx := context.Background()
	tid1, uid1 := uuid.New(), uuid.New()
	tid2, uid2 := uuid.New(), uuid.New()
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid1, uid1, 1))
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid1, uid1, 1))
	// tid1+uid1 capped now.
	require.ErrorIs(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid1, uid1, 1),
		quota.ErrQuotaExceeded)
	// Different tenant: fresh counter.
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid2, uid2, 1))
}

func TestCheckAndIncr_WindowRollover(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{ToolInvokePerMinute: 1})
	tid, uid := uuid.New(), uuid.New()
	// Pin clock to 12:00:30.
	t0 := time.Date(2026, 5, 21, 12, 0, 30, 0, time.UTC)
	svc.SetNowForTest(func() time.Time { return t0 })
	ctx := context.Background()
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid, uid, 1))
	require.ErrorIs(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid, uid, 1),
		quota.ErrQuotaExceeded)
	// Roll the clock into the next minute window — fresh key, allowed again.
	svc.SetNowForTest(func() time.Time { return t0.Add(time.Minute) })
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindToolInvoke, tid, uid, 1))
}

func TestCheckAndIncr_UnsupportedKind(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{LLMTokensPerDay: 100})
	err := svc.CheckAndIncr(context.Background(), "bogus.kind", uuid.New(), uuid.New(), 1)
	require.Error(t, err)
	require.False(t, errors.Is(err, quota.ErrQuotaExceeded))
}

func TestAdjust_AccumulatesPastInitial(t *testing.T) {
	svc, mr := newSvc(t, quota.Limits{LLMTokensPerDay: 1000})
	ctx := context.Background()
	tid, uid := uuid.New(), uuid.New()
	// Reserve 500 estimate.
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, uid, 500))
	// Actual was 700: bump by 200 to reconcile.
	require.NoError(t, svc.Adjust(ctx, quota.KindLLMTokens, tid, uid, 200))
	// The next pre-check sees the reconciled total — try to reserve 400 →
	// 700 + 400 = 1100 > 1000 → reject.
	err := svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, uid, 400)
	require.ErrorIs(t, err, quota.ErrQuotaExceeded)

	// And miniredis keys reflect the count for sanity.
	require.NotEmpty(t, mr.Keys())
}

func TestSandboxCap_Getter(t *testing.T) {
	svc, _ := newSvc(t, quota.Limits{SandboxMaxActive: 7})
	require.Equal(t, 7, svc.SandboxCap())
}
