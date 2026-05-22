package reflection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/yourorg/private-coding-agent/internal/audit"
	pcametrics "github.com/yourorg/private-coding-agent/internal/metrics"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/session"
)

var tracer trace.Tracer = otel.Tracer("internal/reflection")

// ReflectionMarker is the canonical token embedded in the Reflector's system
// prompt. The mock LLM provider in test environments looks for this in the
// system message to return canned JSON; production LLMs see it as just part
// of the instruction.
const ReflectionMarker = "REFLECTION_TASK_V1"

const reflectionSystemPrompt = `You are a memory-extraction agent. From this conversation, extract up to 3
memory items worth saving for future sessions. Return ONLY a JSON array of
objects with fields: type ("profile"|"preference"|"knowledge"|"lesson"),
content (string, <=500 chars), tags (string array, <=5 items),
confidence (0.0-1.0). Return [] if nothing worth saving.
` + ReflectionMarker

// Config controls Reflector behavior. Wired from cfg.Reflection.
type Config struct {
	Enabled              bool
	Model                string
	AutoApproveThreshold float64
	MaxMessagesPerSession int
	MaxCharsPerMessage   int
	WorkerBuffer         int
	WorkerTimeout        time.Duration
}

// ChatCompleter is the modelgw.Gateway subset the Reflector consumes.
// Allows test fakes without dragging the whole gateway.
type ChatCompleter interface {
	ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
		req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
}

// MemoryCreator is the memory.Service subset used during approve / auto-approve.
// Returns the persisted (or deduped) memory id and whether a new row was
// inserted (dedupHit = !Created).
type MemoryCreator interface {
	CreateForReflection(ctx context.Context, tenantID, userID uuid.UUID,
		typ, content string, tags []string, sourceMsgID *uuid.UUID) (memoryID uuid.UUID, dedupHit bool, err error)
}

// MessageLister abstracts the session.MessageRepo so the Reflector can be
// tested without dockertest.
type MessageLister interface {
	List(ctx context.Context, tenantID, sessionID uuid.UUID) ([]session.Message, error)
}

// Reflector turns one archived session into 0..N MemoryProposal rows.
type Reflector struct {
	gw       ChatCompleter
	memSvc   MemoryCreator
	msgs     MessageLister
	repo     *Repo
	audit    audit.Sink
	cfg      Config
	nowFn    func() time.Time
}

// NewReflector wires the dependencies. cfg.MaxMessagesPerSession ≤ 0
// defaults to 20; MaxCharsPerMessage ≤ 0 defaults to 500.
func NewReflector(gw ChatCompleter, memSvc MemoryCreator, msgs MessageLister,
	repo *Repo, sink audit.Sink, cfg Config) *Reflector {
	if cfg.MaxMessagesPerSession <= 0 {
		cfg.MaxMessagesPerSession = 20
	}
	if cfg.MaxCharsPerMessage <= 0 {
		cfg.MaxCharsPerMessage = 500
	}
	return &Reflector{
		gw: gw, memSvc: memSvc, msgs: msgs, repo: repo, audit: sink, cfg: cfg,
		nowFn: time.Now,
	}
}

// rawProposal mirrors the JSON the LLM returns.
type rawProposal struct {
	Type       string   `json:"type"`
	Content    string   `json:"content"`
	Tags       []string `json:"tags"`
	Confidence float64  `json:"confidence"`
}

// Reflect runs the full pipeline for one job. Errors are logged via audit;
// the caller (worker) does not retry.
func (r *Reflector) Reflect(ctx context.Context, job ReflectionJob) error {
	ctx, span := tracer.Start(ctx, "reflection.session",
		trace.WithAttributes(attribute.String("session.id", job.SessionID.String())))
	defer span.End()

	start := time.Now()

	msgs, err := r.msgs.List(ctx, job.TenantID, job.SessionID)
	if err != nil {
		r.recordFailure(ctx, job, "list_messages", err, start)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if len(msgs) == 0 {
		// Empty session — record a complete event with 0 proposals.
		r.recordComplete(ctx, job, 0, start)
		return nil
	}

	chatMsgs := r.buildChatMessages(msgs)

	llmCtx, llmSpan := tracer.Start(ctx, "reflection.llm")
	resp, err := r.gw.ChatCompletion(llmCtx, job.TenantID, job.UserID, modelgw.ChatRequest{
		Model:    r.cfg.Model,
		Messages: chatMsgs,
	})
	llmSpan.End()
	if err != nil {
		r.bumpOutcome(ctx, "llm_failed")
		r.recordFailure(ctx, job, "llm_call", err, start)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	if len(resp.Choices) == 0 {
		err := errors.New("empty choices")
		r.bumpOutcome(ctx, "llm_failed")
		r.recordFailure(ctx, job, "empty_choices", err, start)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	raw := resp.Choices[0].Message.Content
	props, err := parseProposalsJSON(raw)
	if err != nil {
		r.bumpOutcome(ctx, "llm_failed")
		r.recordFailure(ctx, job, "parse_json", err, start)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	persistCtx, persistSpan := tracer.Start(ctx, "reflection.persist")
	count := 0
	for _, rp := range props {
		if !IsValidType(rp.Type) || strings.TrimSpace(rp.Content) == "" {
			continue
		}
		conf := clampConfidence(rp.Confidence)
		tags := rp.Tags
		if tags == nil {
			tags = []string{}
		}
		autoApprove := r.cfg.AutoApproveThreshold > 0 && float64(conf) >= r.cfg.AutoApproveThreshold

		var memoryID *uuid.UUID
		status := StatusPending
		dedupHit := false
		if autoApprove {
			mid, hit, cerr := r.memSvc.CreateForReflection(persistCtx,
				job.TenantID, job.UserID, rp.Type, rp.Content, tags, nil)
			if cerr != nil {
				// Auto-approve failed; fall back to pending row so it surfaces
				// in admin queue.
				r.bumpOutcome(persistCtx, "llm_failed")
				autoApprove = false
			} else {
				status = StatusAutoApproved
				memoryID = &mid
				dedupHit = hit
			}
		}
		sessID := job.SessionID
		p := &MemoryProposal{
			TenantID:    job.TenantID,
			OwnerUserID: job.UserID,
			SessionID:   &sessID,
			Type:        rp.Type,
			Content:     truncate(rp.Content, 500),
			Tags:        tags,
			Confidence:  conf,
			Status:      status,
			MemoryID:    memoryID,
		}
		if status == StatusAutoApproved {
			now := r.nowFn()
			p.DecidedAt = &now
		}
		stored, err := r.repo.Insert(persistCtx, p)
		if err != nil {
			persistSpan.End()
			r.recordFailure(ctx, job, "persist", err, start)
			span.SetStatus(codes.Error, err.Error())
			return err
		}
		count++
		if status == StatusAutoApproved {
			r.bumpOutcome(persistCtx, "auto_approved")
			r.auditProposalDecision(job, stored, "memory.proposal.approve", map[string]any{
				"memory_id": memoryID.String(), "dedup_hit": dedupHit, "by": "auto",
			})
		} else {
			r.bumpOutcome(persistCtx, "created")
		}
		r.auditProposalCreated(job, stored)
	}
	persistSpan.End()

	r.recordComplete(ctx, job, count, start)
	return nil
}

func (r *Reflector) buildChatMessages(msgs []session.Message) []modelgw.ChatMessage {
	limit := r.cfg.MaxMessagesPerSession
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]modelgw.ChatMessage, 0, len(msgs)+1)
	out = append(out, modelgw.ChatMessage{
		Role: modelgw.RoleSystem, Content: reflectionSystemPrompt,
	})
	for _, m := range msgs {
		role := mapRole(m.Role)
		if role == "" {
			continue
		}
		out = append(out, modelgw.ChatMessage{
			Role:    role,
			Content: truncate(m.Content, r.cfg.MaxCharsPerMessage),
		})
	}
	return out
}

// mapRole keeps user/assistant; drops tool/system (Reflector controls its own
// system prompt) so the LLM sees a clean transcript.
func mapRole(r string) modelgw.ChatRole {
	switch r {
	case "user":
		return modelgw.RoleUser
	case "assistant":
		return modelgw.RoleAssistant
	}
	return ""
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func clampConfidence(f float64) float32 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return float32(f)
}

// parseProposalsJSON tolerates LLMs that wrap the array in code fences or
// add a leading word. It locates the outer [...] and json-decodes.
func parseProposalsJSON(raw string) ([]rawProposal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty response")
	}
	// Strip leading ```json fences if present.
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '[')
	end := strings.LastIndexByte(raw, ']')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found")
	}
	var out []rawProposal
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func (r *Reflector) bumpOutcome(ctx context.Context, outcome string) {
	if pcametrics.ReflectionProposalsTotal == nil {
		return
	}
	pcametrics.ReflectionProposalsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
	))
}

func (r *Reflector) recordComplete(ctx context.Context, job ReflectionJob, count int, start time.Time) {
	if r.audit == nil {
		return
	}
	tid := job.TenantID
	uid := job.UserID
	audit.Detached(r.audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action:     "reflection.session.complete",
		Target:     job.SessionID.String(),
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata: map[string]any{
			"proposal_count": count,
		},
	}, nil)
}

func (r *Reflector) recordFailure(ctx context.Context, job ReflectionJob, errClass string, err error, start time.Time) {
	if r.audit == nil {
		return
	}
	tid := job.TenantID
	uid := job.UserID
	audit.Detached(r.audit, audit.Entry{
		OccurredAt: start,
		TenantID:   &tid, UserID: &uid,
		Action:     "reflection.session.failed",
		Target:     job.SessionID.String(),
		DurationMS: int(time.Since(start).Milliseconds()),
		Metadata: map[string]any{
			"error_class": errClass,
			"error":       err.Error(),
		},
	}, nil)
}

func (r *Reflector) auditProposalCreated(job ReflectionJob, p *MemoryProposal) {
	if r.audit == nil {
		return
	}
	tid := job.TenantID
	uid := job.UserID
	audit.Detached(r.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action: "memory.proposal.create",
		Target: p.ID.String(),
		Metadata: map[string]any{
			"confidence": p.Confidence,
			"type":       p.Type,
			"status":     p.Status,
		},
	}, nil)
}

func (r *Reflector) auditProposalDecision(job ReflectionJob, p *MemoryProposal, action string, meta map[string]any) {
	if r.audit == nil {
		return
	}
	tid := job.TenantID
	uid := job.UserID
	audit.Detached(r.audit, audit.Entry{
		TenantID: &tid, UserID: &uid,
		Action:   action,
		Target:   p.ID.String(),
		Metadata: meta,
	}, nil)
}
