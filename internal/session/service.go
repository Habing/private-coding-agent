package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/memory"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/quota"
)

// titleMaxRunes caps auto-derived session titles. Picked to fit in narrow UI
// rails (the SessionList renders titles in a single line); the message body
// is preserved in full in the messages table.
const titleMaxRunes = 60

// AgentEngine is the subset of *agent.Engine that the Service depends on.
// Declared locally for testability (mockEngine in tests).
type AgentEngine interface {
	Run(ctx context.Context, in agent.RunInput, yield func(agent.Event) error) error
}

// Service is the application-layer orchestrator over SessionRepo, MessageRepo,
// and the Agent Engine. Handler / WSHandler call into this layer only.
type Service struct {
	sessions  *SessionRepo
	messages  *MessageRepo
	engine    AgentEngine
	audit     audit.Sink
	sandbox   sandboxRuntime
	quota     *quota.Service
	activeCnt activeSandboxCounter
	memLoader *memory.Loader
}

func NewService(sessions *SessionRepo, messages *MessageRepo, engine AgentEngine) *Service {
	return &Service{sessions: sessions, messages: messages, engine: engine}
}

// WithSandbox wires sandbox create/destroy for session binding (slice 14).
func (s *Service) WithSandbox(rt sandboxRuntime) *Service {
	s.sandbox = rt
	return s
}

// WithQuota applies the same per-tenant active-sandbox cap as POST /sandbox/sessions.
func (s *Service) WithQuota(q *quota.Service, counter activeSandboxCounter) *Service {
	s.quota = q
	s.activeCnt = counter
	return s
}

// WithAuditSink wires an audit.Sink so the service records session.create /
// session.archive entries on successful operations. Returns the receiver for
// chaining. Setter (rather than constructor arg) keeps NewService callers
// in tests untouched.
func (s *Service) WithAuditSink(sink audit.Sink) *Service {
	s.audit = sink
	return s
}

// WithMemoryLoader wires auto-injection on the first user turn (slice 16).
func (s *Service) WithMemoryLoader(l *memory.Loader) *Service {
	s.memLoader = l
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
	sbID, err := s.provisionSandbox(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}

	sess := &Session{
		ID:          uuid.New(),
		TenantID:    tenantID,
		OwnerUserID: userID,
		Title:       req.Title,
		Model:       req.Model,
		Profile:     profile,
		Status:      StatusActive,
		SandboxID:   &sbID,
		SkillIDs:    req.SkillIDs,
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		s.releaseSandbox(ctx, tenantID, &sbID)
		return nil, err
	}
	// Round-trip read so created_at/updated_at are populated.
	out, err := s.sessions.Get(ctx, tenantID, userID, sess.ID)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{
		"model":   req.Model,
		"profile": profile,
	}
	if sess.SandboxID != nil {
		meta["sandbox_id"] = sess.SandboxID.String()
	}
	s.auditSessionEvent(start, tenantID, userID, sess.ID, "session.create", meta)
	if pcametrics.SessionsCreatedTotal != nil {
		pcametrics.SessionsCreatedTotal.Add(ctx, 1,
			metric.WithAttributes(attribute.String("profile", profile)))
	}
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
	sess, err := s.sessions.Get(ctx, tenantID, userID, id)
	if err != nil {
		return err
	}
	if err := s.sessions.Archive(ctx, tenantID, userID, id); err != nil {
		return err
	}
	s.releaseSandbox(ctx, tenantID, sess.SandboxID)
	meta := map[string]any(nil)
	if sess.SandboxID != nil {
		meta = map[string]any{"sandbox_id": sess.SandboxID.String()}
	}
	s.auditSessionEvent(start, tenantID, userID, id, "session.archive", meta)
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

	// Auto-derive title on the first user message when none was supplied at
	// session creation. WebUI does not collect a title up front; without this
	// every session in the list would render as "Untitled". Failures here are
	// logged-and-swallowed: a missing title is cosmetic.
	if sess.Title == "" && !hasPriorUserMessage(history) {
		if title := deriveTitle(content); title != "" {
			if err := s.sessions.UpdateTitle(ctx, tenantID, userID, sid, title); err != nil {
				slog.Warn("session.auto_title", "session_id", sid, "err", err.Error())
			} else {
				sess.Title = title
			}
		}
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
		TenantID:        tenantID,
		UserID:          userID,
		Model:           sess.Model,
		Messages:        chatMsgs,
		ProfileName:     sess.Profile,
		SessionSkillIDs: sess.SkillIDs,
	}
	if sess.SandboxID != nil {
		in.SandboxID = *sess.SandboxID
	}
	if s.memLoader != nil && !hasPriorUserMessage(history) {
		loaded, err := s.memLoader.LoadForSession(ctx, tenantID, userID, content)
		if err != nil {
			slog.Warn("memory.load", "session_id", sid, "err", err.Error())
		} else if loaded.Section != "" {
			in.MemorySection = loaded.Section
			in.MemoryCharCount = loaded.CharCount
			in.MemoryTruncated = loaded.Truncated
			for _, mid := range loaded.IDs {
				in.MemoryIDs = append(in.MemoryIDs, mid.String())
			}
		}
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

// hasPriorUserMessage reports whether the persisted history already contains
// at least one user-role row. We only derive a title on the very first user
// turn so re-sending after the agent fails doesn't keep rewriting it.
func hasPriorUserMessage(history []Message) bool {
	for _, m := range history {
		if m.Role == RoleUser {
			return true
		}
	}
	return false
}

// deriveTitle produces a single-line, rune-bounded excerpt of the first user
// message. Whitespace runs (incl. newlines) collapse to one space; an empty
// result (whitespace-only input) returns "" so the caller skips the update.
func deriveTitle(content string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range content {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	if len(runes) <= titleMaxRunes {
		return cleaned
	}
	return string(runes[:titleMaxRunes]) + "..."
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
