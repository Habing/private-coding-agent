package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/session"
)

// mockEngine implements session.AgentEngine. Each call to Run replays a fixed
// event sequence and records the last received RunInput.
type mockEngine struct {
	mu       sync.Mutex
	events   []agent.Event
	runErr   error
	lastInIn agent.RunInput
	calls    int
}

func (m *mockEngine) Run(ctx context.Context, in agent.RunInput, yield func(agent.Event) error) error {
	m.mu.Lock()
	m.lastInIn = in
	m.calls++
	events := m.events
	m.mu.Unlock()
	for _, e := range events {
		if err := yield(e); err != nil {
			return err
		}
	}
	return m.runErr
}

func newService(t *testing.T) (*session.Service, *mockEngine, uuid.UUID, uuid.UUID) {
	pg := newPool(t)
	tid, uid := fixtures(t, pg)
	eng := &mockEngine{}
	svc := session.NewService(
		session.NewSessionRepo(pg),
		session.NewMessageRepo(pg),
		eng,
	)
	return svc, eng, tid, uid
}

func TestService_CreateSession(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()

	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{
		Model: "default-mock:gpt-4o", Title: "hi",
	})
	require.NoError(t, err)
	require.Equal(t, "default-mock:gpt-4o", s.Model)
	require.Equal(t, "coding", s.Profile)
	require.Equal(t, session.StatusActive, s.Status)
	require.False(t, s.CreatedAt.IsZero())
}

func TestService_CreateSession_ModelRequired(t *testing.T) {
	svc, _, tid, uid := newService(t)
	_, err := svc.CreateSession(context.Background(), tid, uid, session.CreateRequest{})
	require.ErrorIs(t, err, session.ErrModelRequired)
}

func TestService_GetSession_CrossTenant(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)

	_, err = svc.GetSession(ctx, uuid.New(), uid, s.ID)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
	_, err = svc.GetSession(ctx, tid, uuid.New(), s.ID)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestService_ListSessions(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
		require.NoError(t, err)
	}
	list, err := svc.ListSessions(ctx, tid, uid)
	require.NoError(t, err)
	require.Len(t, list, 3)
}

func TestService_ArchiveSession(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)

	require.NoError(t, svc.ArchiveSession(ctx, tid, uid, s.ID))
	got, err := svc.GetSession(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, session.StatusArchived, got.Status)
}

func TestService_SendMessage_HappyPath(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "default-mock:gpt-4o"})
	require.NoError(t, err)

	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "hello", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "hello", FinishReason: "stop"},
	}

	var seen []agent.Event
	err = svc.SendMessage(ctx, tid, uid, s.ID, "hi", func(e agent.Event) error {
		seen = append(seen, e)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 2)

	// Engine was invoked with the user message appended to the (empty) history.
	require.Equal(t, "default-mock:gpt-4o", eng.lastInIn.Model)
	require.Equal(t, "coding", eng.lastInIn.ProfileName)
	require.Len(t, eng.lastInIn.Messages, 1)
	require.Equal(t, modelgw.RoleUser, eng.lastInIn.Messages[0].Role)
	require.Equal(t, "hi", eng.lastInIn.Messages[0].Content)

	// Messages persisted: 1 user + 1 assistant (final is skipped).
	msgs, err := svc.ListMessages(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Len(t, msgs, 2)
	require.Equal(t, session.RoleUser, msgs[0].Role)
	require.Equal(t, "hi", msgs[0].Content)
	require.Equal(t, session.RoleAssistant, msgs[1].Role)
	require.Equal(t, "hello", msgs[1].Content)
}

func TestService_SendMessage_ToolChain(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)

	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, FinishReason: "tool_calls",
			ToolCalls: []modelgw.ToolCall{{
				ID: "c1", Type: "function",
				Function: modelgw.ToolCallFunc{Name: "fs.list", Arguments: `{}`},
			}}},
		{Kind: agent.EventToolCall, Step: 1, ToolCallID: "c1", ToolName: "fs.list",
			ToolInput: json.RawMessage(`{}`)},
		{Kind: agent.EventToolResult, Step: 1, ToolCallID: "c1", ToolName: "fs.list",
			ToolOutput: json.RawMessage(`{"entries":[]}`)},
		{Kind: agent.EventAssistantMessage, Step: 2, Text: "done", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 2, Text: "done", FinishReason: "stop"},
	}

	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, "list files", nil))

	msgs, err := svc.ListMessages(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	// user + assistant(tool_calls) + tool + assistant(final) = 4 (tool_call + final skipped)
	require.Len(t, msgs, 4)
	require.Equal(t, session.RoleUser, msgs[0].Role)
	require.Equal(t, session.RoleAssistant, msgs[1].Role)
	require.Contains(t, string(msgs[1].ToolCalls), `"c1"`)
	require.Equal(t, session.RoleTool, msgs[2].Role)
	require.Equal(t, "c1", msgs[2].ToolCallID)
	require.Equal(t, `{"entries":[]}`, msgs[2].Content)
	require.Equal(t, session.RoleAssistant, msgs[3].Role)
	require.Equal(t, "done", msgs[3].Content)
}

func TestService_SendMessage_EmptyContent(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	err = svc.SendMessage(ctx, tid, uid, s.ID, "", nil)
	require.ErrorIs(t, err, session.ErrEmptyContent)
}

func TestService_SendMessage_Archived(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	require.NoError(t, svc.ArchiveSession(ctx, tid, uid, s.ID))
	err = svc.SendMessage(ctx, tid, uid, s.ID, "hi", nil)
	require.ErrorIs(t, err, session.ErrSessionArchived)
}

func TestService_SendMessage_NotFound_CrossTenant(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	err = svc.SendMessage(ctx, uuid.New(), uid, s.ID, "hi", nil)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
}

func TestService_SendMessage_OnEventAbort(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "hi"},
		{Kind: agent.EventFinal, Step: 1, Text: "hi"},
	}
	abortErr := errors.New("client disconnected")
	err = svc.SendMessage(ctx, tid, uid, s.ID, "hi", func(e agent.Event) error {
		return abortErr
	})
	require.ErrorIs(t, err, abortErr)
}

func TestService_SendMessage_RehydratesHistory(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)

	// First turn.
	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "round1", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "round1", FinishReason: "stop"},
	}
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, "first", nil))

	// Second turn — engine sees history (user1, assistant1, user2).
	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "round2", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "round2", FinishReason: "stop"},
	}
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, "second", nil))

	require.Len(t, eng.lastInIn.Messages, 3)
	require.Equal(t, "first", eng.lastInIn.Messages[0].Content)
	require.Equal(t, "round1", eng.lastInIn.Messages[1].Content)
	require.Equal(t, "second", eng.lastInIn.Messages[2].Content)
}

func TestService_SendMessage_AutoTitle(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	require.Equal(t, "", s.Title)

	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "ok", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "ok", FinishReason: "stop"},
	}
	// Multi-line, whitespace-heavy first message: should collapse to single-line
	// excerpt.
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID,
		"help me\n\n  refactor   this code please", nil))

	got, err := svc.GetSession(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, "help me refactor this code please", got.Title)

	// A subsequent send should not overwrite the existing title.
	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "ok2", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "ok2", FinishReason: "stop"},
	}
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, "another question", nil))
	got, err = svc.GetSession(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, "help me refactor this code please", got.Title)
}

func TestService_SendMessage_AutoTitle_DoesNotOverwriteSupplied(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{
		Model: "m", Title: "preset title",
	})
	require.NoError(t, err)

	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "ok", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "ok", FinishReason: "stop"},
	}
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, "first message", nil))

	got, err := svc.GetSession(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, "preset title", got.Title)
}

func TestService_SendMessage_AutoTitle_LongMessageTruncates(t *testing.T) {
	svc, eng, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)

	eng.events = []agent.Event{
		{Kind: agent.EventAssistantMessage, Step: 1, Text: "ok", FinishReason: "stop"},
		{Kind: agent.EventFinal, Step: 1, Text: "ok", FinishReason: "stop"},
	}
	long := strings.Repeat("a", 200)
	require.NoError(t, svc.SendMessage(ctx, tid, uid, s.ID, long, nil))

	got, err := svc.GetSession(ctx, tid, uid, s.ID)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("a", 60)+"...", got.Title)
}

func TestService_ListMessages_CrossTenantReturns404(t *testing.T) {
	svc, _, tid, uid := newService(t)
	ctx := context.Background()
	s, err := svc.CreateSession(ctx, tid, uid, session.CreateRequest{Model: "m"})
	require.NoError(t, err)
	_, err = svc.ListMessages(ctx, uuid.New(), uid, s.ID)
	require.ErrorIs(t, err, session.ErrSessionNotFound)
}
