package session_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/session"
)

type spySessionSink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spySessionSink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func (s *spySessionSink) entries() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Entry, len(s.got))
	copy(out, s.got)
	return out
}

func (s *spySessionSink) findByAction(action string) (audit.Entry, bool) {
	for _, e := range s.entries() {
		if e.Action == action {
			return e, true
		}
	}
	return audit.Entry{}, false
}

func TestService_CreateSession_EmitsAudit(t *testing.T) {
	svc, _, _, tid, uid := newService(t)
	sink := &spySessionSink{}
	svc.WithAuditSink(sink)
	ctx := context.Background()

	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{
		Model: "default-mock:gpt-4o", Title: "hi",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, ok := sink.findByAction("session.create")
		return ok
	}, time.Second, 10*time.Millisecond)
	e, _ := sink.findByAction("session.create")
	require.Equal(t, s.ID.String(), e.Target)
	require.Equal(t, "default-mock:gpt-4o", e.Metadata["model"])
	require.Equal(t, "coding", e.Metadata["profile"])
	require.NotNil(t, e.TenantID)
	require.Equal(t, tid, *e.TenantID)
	require.NotNil(t, e.UserID)
	require.Equal(t, uid, *e.UserID)
}

func TestService_CreateSession_ModelRequired_NoAudit(t *testing.T) {
	svc, _, _, tid, uid := newService(t)
	sink := &spySessionSink{}
	svc.WithAuditSink(sink)

	_, err := svc.CreateSession(context.Background(), tid, uid, session.CreateRequest{})
	require.Error(t, err)
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, sink.entries(), "validation error must not emit audit")
}

func TestService_ArchiveSession_EmitsAudit(t *testing.T) {
	svc, _, _, tid, uid := newService(t)
	sink := &spySessionSink{}
	svc.WithAuditSink(sink)
	ctx := context.Background()

	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	require.NoError(t, svc.ArchiveSession(ctx, tid, uid, s.ID))

	require.Eventually(t, func() bool {
		_, ok := sink.findByAction("session.archive")
		return ok
	}, time.Second, 10*time.Millisecond)
	e, _ := sink.findByAction("session.archive")
	require.Equal(t, s.ID.String(), e.Target)
	require.NotNil(t, e.TenantID)
	require.Equal(t, tid, *e.TenantID)
}

func TestService_NilSink_Safe(t *testing.T) {
	svc, _, _, tid, uid := newService(t)
	// Not setting any sink.
	s, err := svc.CreateSession(context.Background(), tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	require.NoError(t, svc.ArchiveSession(context.Background(), tid, uid, s.ID))
}
