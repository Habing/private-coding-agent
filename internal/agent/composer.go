package agent

import (
	"context"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/skills"
)

// ComposeInput is the projection of RunInput needed by a ContextComposer.
// Slice 13 (memory auto-inject) will append fields here, not split the
// interface.
type ComposeInput struct {
	TenantID        uuid.UUID
	UserID          uuid.UUID
	Profile         Profile
	RunSkillIDs     []string
	SessionSkillIDs []string
}

// ComposeMeta is per-Run telemetry/audit metadata produced by the composer.
type ComposeMeta struct {
	SkillIDs  []string
	CharCount int
	Truncated bool
}

// ContextComposer builds the system-layer prefix for an agent Run. 12a
// implements the Skill side; future slices add Memory auto-inject by
// composing additional ComposeInput fields.
type ContextComposer interface {
	ComposeSystem(ctx context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error)
}

// SkillComposer is the production ContextComposer used when the Skills
// subsystem is enabled. Resolver / Config are taken at construction time.
type SkillComposer struct {
	resolver *skills.Resolver
	cfg      skills.Config
}

func NewSkillComposer(resolver *skills.Resolver, cfg skills.Config) *SkillComposer {
	return &SkillComposer{resolver: resolver, cfg: cfg}
}

func (c *SkillComposer) ComposeSystem(_ context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error) {
	resolved := c.resolver.Resolve(skills.ResolveInput{
		RunSkillIDs:     in.RunSkillIDs,
		SessionSkillIDs: in.SessionSkillIDs,
		ProfileSkillIDs: in.Profile.SkillIDs,
	})
	res := skills.BuildSystemMessages(in.Profile.SystemPrompt, resolved, c.cfg.MaxInjectedChars)
	return res.Messages, ComposeMeta{
		SkillIDs:  res.SkillIDs,
		CharCount: res.CharCount,
		Truncated: res.Truncated,
	}, nil
}

// NoopComposer is the fallback for tests and when skills.enabled=false:
// it returns the Profile.SystemPrompt as a single system message and no
// Skill metadata, preserving the pre-Slice-12 behavior byte-for-byte.
type NoopComposer struct{}

func (NoopComposer) ComposeSystem(_ context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error) {
	if in.Profile.SystemPrompt == "" {
		return nil, ComposeMeta{}, nil
	}
	return []modelgw.ChatMessage{{
		Role:    modelgw.RoleSystem,
		Content: in.Profile.SystemPrompt,
	}}, ComposeMeta{}, nil
}
