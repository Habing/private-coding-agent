// Package orchestrator implements Slice 21a: a pre-Run routing pass that
// inspects the user's latest message against a small static rule set and
// optionally injects a "routing hint" system message into the agent's
// outgoing prompt. It does NOT bypass the ReAct loop; the LLM remains free to
// ignore the hint. Two observable side effects:
//
//   - one audit row per Run (action=orchestrator.route, even on no_match)
//   - one counter increment (pca_orchestrator_routes_total)
//
// Rules live in YAML config (not DB). v1 keeps the engine a pure function:
// it returns a Decision; the caller emits audit + metric. v2+ may add hot
// reload or per-tenant rules.
package orchestrator

import (
	"context"

	"github.com/google/uuid"
)

// Decision is the verdict produced by Engine.Route for a single agent Run.
type Decision struct {
	// Matched is true if any rule fired OR a non-empty default_hint kicked in.
	Matched bool
	// RuleName is the name of the matching rule, or "" if Matched is false,
	// or "default" if default_hint kicked in.
	RuleName string
	// Type is the suggested target kind: "tool" | "workflow" | "sub_agent" |
	// "skill". Empty when Matched is false.
	Type string
	// Target is the canonical name (tool name, workflow slug, profile name,
	// or skill key). Empty when Matched is false.
	Target string
	// Hint is the system message body to inject. Empty means "audit but don't
	// inject" — also true when Matched is false.
	Hint string
	// MatchedOn names the match dimension that fired:
	// "content_regex" | "content_contains" | "default" | "".
	MatchedOn string
}

// RouteInput is what the caller hands to Engine.Route. SessionID is optional
// (some pure tool-mode flows don't have a session).
type RouteInput struct {
	Profile     string
	UserContent string
	SessionID   *uuid.UUID
}

// Rule is the YAML-defined unit. At least one field of Match must be set;
// Suggest.Type + Target + Hint are required when the rule should produce a
// usable Decision (an empty Hint is allowed: audit-only rule).
type Rule struct {
	Name    string      `mapstructure:"name"`
	Match   RuleMatch   `mapstructure:"match"`
	Suggest RuleSuggest `mapstructure:"suggest"`
}

// RuleMatch fields are AND-combined. A rule with no fields set never matches.
type RuleMatch struct {
	Profile         []string `mapstructure:"profile"`
	ContentRegex    string   `mapstructure:"content_regex"`
	ContentContains string   `mapstructure:"content_contains"`
}

// RuleSuggest describes what the rule recommends.
type RuleSuggest struct {
	Type   string `mapstructure:"type"`
	Target string `mapstructure:"target"`
	Hint   string `mapstructure:"hint"`
}

// Router is the narrow interface agent.Engine consumes. Keeping it small lets
// tests inject a fake without pulling in the whole config dependency chain.
type Router interface {
	Route(ctx context.Context, in RouteInput) Decision
	InjectHintEnabled() bool
}
