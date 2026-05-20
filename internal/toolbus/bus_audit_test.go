package toolbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

type spyBusSink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spyBusSink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func (s *spyBusSink) entries() []audit.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Entry, len(s.got))
	copy(out, s.got)
	return out
}

func TestBus_Invoke_Error_EmitsAudit(t *testing.T) {
	tool := &mockTool{name: "t.audit-err", schema: json.RawMessage(objSchemaWithX),
		invokeErr: errors.New("downstream boom")}
	bus, _, _ := busWith(t, tool)
	sink := &spyBusSink{}
	bus.WithAuditSink(sink)
	tid, uid := uuid.New(), uuid.New()

	_, err := bus.Invoke(context.Background(), tid, uid,
		"t.audit-err", json.RawMessage(`{"x":1}`))
	require.Error(t, err)

	require.Eventually(t, func() bool { return len(sink.entries()) >= 1 },
		time.Second, 10*time.Millisecond)
	e := sink.entries()[0]
	require.Equal(t, "tool.invoke.error", e.Action)
	require.Equal(t, "t.audit-err", e.Target)
	require.NotNil(t, e.TenantID)
	require.Equal(t, tid, *e.TenantID)
	require.NotNil(t, e.UserID)
	require.Equal(t, uid, *e.UserID)
	require.Equal(t, "other", e.Metadata["error_class"])
}

func TestBus_Invoke_OK_NoAudit(t *testing.T) {
	tool := &mockTool{name: "t.audit-ok", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"ok":true}`)}
	bus, _, _ := busWith(t, tool)
	sink := &spyBusSink{}
	bus.WithAuditSink(sink)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.audit-ok", json.RawMessage(`{"x":1}`))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)
	require.Empty(t, sink.entries(), "ok invocation must not emit audit")
}

func TestBus_Invoke_NilSink_Safe(t *testing.T) {
	tool := &mockTool{name: "t.nil-sink", schema: json.RawMessage(objSchemaWithX),
		invokeErr: errors.New("boom")}
	bus, _, _ := busWith(t, tool)
	// No WithAuditSink call.

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.nil-sink", json.RawMessage(`{"x":1}`))
	require.Error(t, err)
}
