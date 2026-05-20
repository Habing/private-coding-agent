package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// AgentEngine is the subset of *agent.Engine that the Service depends on.
// Declared locally for testability (mockEngine in tests).
type AgentEngine interface {
	Run(ctx context.Context, in agent.RunInput, yield func(agent.Event) error) error
}

// Service is the application-layer orchestrator over SessionRepo, MessageRepo,
// and the Agent Engine. Handler / WSHandler call into this layer only.
type Service struct {
	sessions *SessionRepo
	messages *MessageRepo
	engine   AgentEngine
	audit    audit.Sink
}

func NewService(sessions *SessionRepo, messages *MessageRepo, engine AgentEngine) *Service {
	return &Service{sessions: sessions, messages: messages, engine: engine}
}

// WithAuditSink wires an audit.Sink so the service records session.create /
// session.archive entries on successful operations. Returns the receiver for
// chaining. Setter (rather than constructor arg) keeps NewService callers
// in tests untouched.
func (s *Service) WithAuditSink(sink audit.Sink) *Service {
	s.audit = sink
	return s
}

func (s *Service) auditSessionEvent(start time.Time, tenantID, userID, sid uuid.UUID, action string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	tid := tenantID
	uid := userID
	audit.Detached(s.audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action:     action,
		Target:     sid.String(),
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata:   meta,
	}, nil)
}

// CreateSession persists a new active session. Model is required; profile
// defaults to "coding".
func (s *Service) CreateSession(ctx context.Context, tenantID, userID uuid.UUID,
	req CreateRequest) (*Session, error) {
	start := time.Now()
	if req.Model == "" {
		return nil, ErrModelRequired
	}
	profile := req.Profile
	if profile == "" {
		profile = DefaultProfile
	}
	sess := &Session{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OwnerUserID: userID,
		Title:       req.Title,
		Model:       req.Model,
		Profile:     profile,
		Status:      StatusActive,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}
	// Round-trip read so created_at/updated_at are populated.
	out, err := s.sessions.Get(ctx, tenantID, userID, sess.ID)
	if err != nil {
		return nil, err
	}
	s.auditSessionEvent(start, tenantID, userID, sess.ID, "session.create", map[string]any{
		"model":   req.Model,
		"profile": profile,
	})
	return out, nil
}

// ListSessions returns all sessions owned by userID under tenantID.
func (s *Service) ListSessions(ctx context.Context, tenantID, userID uuid.UUID) ([]Session, error) {
	return s.sessions.List(ctx, tenantID, userID)
}

// GetSession returns one session; cross-tenant / cross-owner reads return
// ErrSessionNotFound.
func (s *Service) GetSession(ctx context.Context, tenantID, userID, id uuid.UUID) (*Session, error) {
	return s.sessions.Get(ctx, tenantID, userID, id)
}

// ArchiveSession sets the session status to "archived".
func (s *Service) ArchiveSession(ctx context.Context, tenantID, userID, id uuid.UUID) error {
	start := time.Now()
	if err := s.sessions.Archive(ctx, tenantID, userID, id); err != nil {
		return err
	}
	s.auditSessionEvent(start, tenantID, userID, id, "session.archive", nil)
	return nil
}

// ListMessages returns all messages of a session in seq order. The session
// must exist and belong to the caller; otherwise ErrSessionNotFound.
func (s *Service) ListMessages(ctx context.Context, tenantID, userID, sid uuid.UUID) ([]Message, error) {
	if _, err := s.sessions.Get(ctx, tenantID, userID, sid); err != nil {
		return nil, err
	}
	return s.messages.List(ctx, tenantID, sid)
}

// SendMessage appends a user message, drives the agent engine to completion,
// persists every relevant Event as a Message, and invokes onEvent for each
// Event (allowing the caller to stream them to a websocket client).
//
// If onEvent returns an error the engine run is aborted and that error is
// returned. If the engine itself returns an error, SendMessage returns the
// wrapped error after the user message has already been persisted.
func (s *Service) SendMessage(ctx context.Context, tenantID, userID, sid uuid.UUID,
	content string, onEvent func(agent.Event) error) error {
	if content == "" {
		return ErrEmptyContent
	}
	sess, err := s.sessions.Get(ctx, tenantID, userID, sid)
	if err != nil {
		return err
	}
	if sess.Status == StatusArchived {
		return ErrSessionArchived
	}

	history, err := s.messages.List(ctx, tenantID, sid)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}

	userSeq, err := s.messages.NextSeq(ctx, sid)
	if err != nil {
		return err
	}
	userMsg := &Message{
		ID: uuid.New(), SessionID: sid, TenantID: tenantID, Seq: userSeq,
		Role: RoleUser, Content: content,
	}
	if err := s.messages.Append(ctx, userMsg); err != nil {
		return fmt.Errorf("append user message: %w", err)
	}

	chatMsgs := historyToChatMessages(history)
	chatMsgs = append(chatMsgs, modelgw.ChatMessage{Role: modelgw.RoleUser, Content: content})

	in := agent.RunInput{
		TenantID:    tenantID,
		UserID:      userID,
		Model:       sess.Model,
		Messages:    chatMsgs,
		ProfileName: sess.Profile,
	}

	yield := func(evt agent.Event) error {
		if msg, persist := eventToMessage(sid, tenantID, evt); persist {
			seq, err := s.messages.NextSeq(ctx, sid)
			if err != nil {
				return fmt.Errorf("next seq: %w", err)
			}
			msg.Seq = seq
			if err := s.messages.Append(ctx, msg); err != nil {
				return fmt.Errorf("append message: %w", err)
			}
		}
		if onEvent != nil {
			return onEvent(evt)
		}
		return nil
	}

	if err := s.engine.Run(ctx, in, yield); err != nil {
		return err
	}
	return nil
}

// historyToChatMessages converts persisted messages into ChatMessages suitable
// for the agent engine. tool_calls JSON is unmarshalled into []ToolCall.
func historyToChatMessages(history []Message) []modelgw.ChatMessage {
	out := make([]modelgw.ChatMessage, 0, len(history))
	for _, m := range history {
		role := modelgw.ChatRole(m.Role)
		cm := modelgw.ChatMessage{Role: role, Content: m.Content}
		switch m.Role {
		case RoleTool:
			cm.ToolCallID = m.ToolCallID
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				var calls []modelgw.ToolCall
				if err := json.Unmarshal(m.ToolCalls, &calls); err == nil {
					cm.ToolCalls = calls
				}
			}
		}
		out = append(out, cm)
	}
	return out
}

// eventToMessage decides whether an Event should be persisted, and produces
// the row. tool_call and final events are intentionally skipped: the former is
// duplicated information from the preceding assistant_message.tool_calls; the
// latter is the same content as the last assistant_message.
func eventToMessage(sid, tid uuid.UUID, evt agent.Event) (*Message, bool) {
	switch evt.Kind {
	case agent.EventAssistantMessage:
		m := &Message{
			ID: uuid.New(), SessionID: sid, TenantID: tid,
			Role:    RoleAssistant,
			Content: evt.Text,
		}
		if len(evt.ToolCalls) > 0 {
			if b, err := json.Marshal(evt.ToolCalls); err == nil {
				m.ToolCalls = b
			}
		}
		if meta, err := json.Marshal(map[string]any{
			"kind":          string(evt.Kind),
			"step":          evt.Step,
			"finish_reason": evt.FinishReason,
		}); err == nil {
			m.Metadata = meta
		}
		return m, true
	case agent.EventToolResult:
		m := &Message{
			ID: uuid.New(), SessionID: sid, TenantID: tid,
			Role:       RoleTool,
			ToolCallID: evt.ToolCallID,
			Content:    string(evt.ToolOutput),
		}
		if evt.ToolError != "" {
			// Store the error text in content; mark in metadata.
			m.Content = evt.ToolError
		}
		if meta, err := json.Marshal(map[string]any{
			"kind":          string(evt.Kind),
			"step":          evt.Step,
			"tool_name":     evt.ToolName,
			"truncated":     evt.Truncated,
			"original_size": evt.OriginalSize,
			"tool_error":    evt.ToolError,
		}); err == nil {
			m.Metadata = meta
		}
		return m, true
	case agent.EventError:
		m := &Message{
			ID: uuid.New(), SessionID: sid, TenantID: tid,
			Role:    RoleSystem,
			Content: evt.Text,
		}
		if meta, err := json.Marshal(map[string]any{
			"kind":          string(evt.Kind),
			"step":          evt.Step,
			"finish_reason": evt.FinishReason,
		}); err == nil {
			m.Metadata = meta
		}
		return m, true
	}
	return nil, false
}
