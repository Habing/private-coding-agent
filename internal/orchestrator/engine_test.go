package orchestrator

import (
	"context"
	"testing"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newTestEngine(t *testing.T, cfg Config) *Engine {
	t.Helper()
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func TestEngine_Disabled_ReturnsZero(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: false,
		Rules: []Rule{{
			Name:    "always",
			Match:   RuleMatch{ContentContains: "foo"},
			Suggest: RuleSuggest{Type: "tool", Target: "fs.list", Hint: "h"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{UserContent: "foo bar"})
	if d.Matched {
		t.Fatalf("disabled engine must return zero Decision, got %+v", d)
	}
	if e.Enabled() {
		t.Fatalf("Enabled() should be false")
	}
}

func TestEngine_ContentContains_Hit(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled:    true,
		InjectHint: true,
		Rules: []Rule{{
			Name:    "marker",
			Match:   RuleMatch{ContentContains: "E2E_ORCHESTRATOR_HINT_V1"},
			Suggest: RuleSuggest{Type: "tool", Target: "fs.list", Hint: "ORCHESTRATOR_E2E_HINT_DELIVERED"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{
		Profile:     "coding",
		UserContent: "please test E2E_ORCHESTRATOR_HINT_V1 path",
	})
	if !d.Matched {
		t.Fatalf("expected Matched=true, got %+v", d)
	}
	if d.RuleName != "marker" || d.Type != "tool" || d.Target != "fs.list" {
		t.Fatalf("wrong decision: %+v", d)
	}
	if d.Hint == "" {
		t.Fatalf("Hint should be populated")
	}
	if d.MatchedOn != "content_contains" {
		t.Fatalf("MatchedOn = %q, want content_contains", d.MatchedOn)
	}
}

func TestEngine_ContentRegex_Hit(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: true,
		Rules: []Rule{{
			Name:    "review-route",
			Match:   RuleMatch{ContentRegex: "(?i)code review|审查"},
			Suggest: RuleSuggest{Type: "workflow", Target: "code-review", Hint: "use workflow"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{UserContent: "Please do a Code Review on this PR"})
	if !d.Matched || d.MatchedOn != "content_regex" {
		t.Fatalf("regex hit expected, got %+v", d)
	}
	if d.RuleName != "review-route" {
		t.Fatalf("RuleName = %q", d.RuleName)
	}
}

func TestEngine_NoMatch(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: true,
		Rules: []Rule{{
			Name:    "marker",
			Match:   RuleMatch{ContentContains: "ABSENT"},
			Suggest: RuleSuggest{Type: "tool", Target: "x", Hint: "h"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{UserContent: "totally unrelated"})
	if d.Matched {
		t.Fatalf("no_match expected, got %+v", d)
	}
	if d.RuleName != "" || d.Hint != "" || d.Target != "" {
		t.Fatalf("zero Decision expected, got %+v", d)
	}
}

func TestEngine_ProfileFilter_Excludes(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: true,
		Rules: []Rule{{
			Name: "coding-only",
			Match: RuleMatch{
				Profile:         []string{"coding"},
				ContentContains: "deploy",
			},
			Suggest: RuleSuggest{Type: "workflow", Target: "deploy", Hint: "h"},
		}},
	})
	// content matches but profile excludes
	d := e.Route(context.Background(), RouteInput{
		Profile:     "review",
		UserContent: "ready to deploy",
	})
	if d.Matched {
		t.Fatalf("profile mismatch should block, got %+v", d)
	}
	// profile matches → fires
	d2 := e.Route(context.Background(), RouteInput{
		Profile:     "coding",
		UserContent: "ready to deploy",
	})
	if !d2.Matched {
		t.Fatalf("profile match should fire, got %+v", d2)
	}
}

func TestEngine_EmptyProfileMatchesAny(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: true,
		Rules: []Rule{{
			Name:    "any",
			Match:   RuleMatch{ContentContains: "x"},
			Suggest: RuleSuggest{Type: "tool", Target: "y", Hint: "h"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{Profile: "", UserContent: "xxx"})
	if !d.Matched {
		t.Fatalf("rule with no profile filter should match any profile (incl. empty)")
	}
}

func TestEngine_InjectHintToggle(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled:    true,
		InjectHint: false,
		Rules: []Rule{{
			Name:    "x",
			Match:   RuleMatch{ContentContains: "x"},
			Suggest: RuleSuggest{Type: "tool", Target: "y", Hint: "h"},
		}},
	})
	if e.InjectHintEnabled() {
		t.Fatalf("InjectHintEnabled should be false")
	}
	// Engine still returns the Decision; suppression is the caller's job.
	d := e.Route(context.Background(), RouteInput{UserContent: "xxx"})
	if !d.Matched || d.Hint == "" {
		t.Fatalf("Route should still produce Hint regardless of inject flag, got %+v", d)
	}
}

func TestEngine_DefaultHint_OnNoMatch(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled:     true,
		DefaultHint: "fallback hint",
		Rules: []Rule{{
			Name:    "specific",
			Match:   RuleMatch{ContentContains: "specific-token"},
			Suggest: RuleSuggest{Type: "tool", Target: "x", Hint: "h"},
		}},
	})
	d := e.Route(context.Background(), RouteInput{UserContent: "nothing relevant"})
	if !d.Matched || d.RuleName != "default" || d.MatchedOn != "default" {
		t.Fatalf("default hint should fire, got %+v", d)
	}
	if d.Hint != "fallback hint" {
		t.Fatalf("Hint = %q", d.Hint)
	}
	if d.Type != "" || d.Target != "" {
		t.Fatalf("default decision must not carry Type/Target, got %+v", d)
	}
}

func TestEngine_RuleOrder_FirstWins(t *testing.T) {
	e := newTestEngine(t, Config{
		Enabled: true,
		Rules: []Rule{
			{Name: "first", Match: RuleMatch{ContentContains: "both"},
				Suggest: RuleSuggest{Type: "tool", Target: "a", Hint: "ha"}},
			{Name: "second", Match: RuleMatch{ContentContains: "both"},
				Suggest: RuleSuggest{Type: "tool", Target: "b", Hint: "hb"}},
		},
	})
	d := e.Route(context.Background(), RouteInput{UserContent: "both apply"})
	if d.RuleName != "first" {
		t.Fatalf("first rule should win, got %+v", d)
	}
}

func TestNewEngine_RejectsBadRegex(t *testing.T) {
	_, err := NewEngine(Config{
		Enabled: true,
		Rules: []Rule{{
			Name:    "bad",
			Match:   RuleMatch{ContentRegex: "(unclosed"},
			Suggest: RuleSuggest{Type: "tool", Target: "x", Hint: "h"},
		}},
	})
	if err == nil {
		t.Fatalf("expected compile error on bad regex")
	}
}

func TestNewEngine_RejectsEmptyMatch(t *testing.T) {
	_, err := NewEngine(Config{
		Enabled: true,
		Rules: []Rule{{
			Name:    "empty",
			Match:   RuleMatch{}, // no profile, no content predicate
			Suggest: RuleSuggest{Type: "tool", Target: "x", Hint: "h"},
		}},
	})
	if err == nil {
		t.Fatalf("expected error on rule with no match predicates")
	}
}

func TestNewEngine_AutoGeneratesName(t *testing.T) {
	e, err := NewEngine(Config{
		Enabled: true,
		Rules:   []Rule{{Match: RuleMatch{ContentContains: "x"}, Suggest: RuleSuggest{Hint: "h"}}},
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	d := e.Route(context.Background(), RouteInput{UserContent: "xxx"})
	if d.RuleName != "rule-0" {
		t.Fatalf("expected rule-0, got %q", d.RuleName)
	}
}

func TestPrependSystemHint_EmptyHintNoOp(t *testing.T) {
	in := []modelgw.ChatMessage{
		{Role: modelgw.RoleSystem, Content: "sys1"},
		{Role: modelgw.RoleUser, Content: "u1"},
	}
	out := PrependSystemHint(in, "")
	if len(out) != 2 {
		t.Fatalf("empty hint should be no-op, got len=%d", len(out))
	}
}

func TestPrependSystemHint_InsertsAfterSystemBlock(t *testing.T) {
	in := []modelgw.ChatMessage{
		{Role: modelgw.RoleSystem, Content: "skill"},
		{Role: modelgw.RoleSystem, Content: "memory"},
		{Role: modelgw.RoleUser, Content: "hi"},
	}
	out := PrependSystemHint(in, "ROUTING")
	if len(out) != 4 {
		t.Fatalf("len=%d", len(out))
	}
	if out[0].Content != "skill" || out[1].Content != "memory" {
		t.Fatalf("existing systems clobbered: %+v", out)
	}
	if out[2].Role != modelgw.RoleSystem || out[2].Content != "ROUTING" {
		t.Fatalf("hint not at index 2: %+v", out)
	}
	if out[3].Role != modelgw.RoleUser {
		t.Fatalf("user message displaced: %+v", out)
	}
}

func TestPrependSystemHint_NoExistingSystems(t *testing.T) {
	in := []modelgw.ChatMessage{
		{Role: modelgw.RoleUser, Content: "hi"},
	}
	out := PrependSystemHint(in, "ROUTING")
	if len(out) != 2 || out[0].Role != modelgw.RoleSystem || out[1].Role != modelgw.RoleUser {
		t.Fatalf("expected [system, user], got %+v", out)
	}
}

func TestPrependSystemHint_AllSystems(t *testing.T) {
	in := []modelgw.ChatMessage{
		{Role: modelgw.RoleSystem, Content: "a"},
		{Role: modelgw.RoleSystem, Content: "b"},
	}
	out := PrependSystemHint(in, "ROUTING")
	if len(out) != 3 || out[2].Content != "ROUTING" {
		t.Fatalf("hint should go after existing systems at end, got %+v", out)
	}
}

func TestPrependSystemHint_DoesNotMutateInput(t *testing.T) {
	in := []modelgw.ChatMessage{
		{Role: modelgw.RoleSystem, Content: "sys"},
		{Role: modelgw.RoleUser, Content: "u"},
	}
	_ = PrependSystemHint(in, "ROUTING")
	if len(in) != 2 || in[0].Content != "sys" || in[1].Content != "u" {
		t.Fatalf("input was mutated: %+v", in)
	}
}
