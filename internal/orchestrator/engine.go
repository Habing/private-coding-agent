package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Config mirrors config.OrchestratorConfig so the orchestrator package stays
// import-free from internal/config (which already imports skills). Caller
// builds this struct from cfg.Orchestrator.
type Config struct {
	Enabled     bool
	InjectHint  bool
	DefaultHint string
	Rules       []Rule
}

// Engine is constructed once at startup and is safe for concurrent use:
// after NewEngine returns it is immutable.
type Engine struct {
	enabled     bool
	injectHint  bool
	defaultHint string
	rules       []compiledRule
}

type compiledRule struct {
	name        string
	profiles    map[string]struct{} // empty → any profile
	regex       *regexp.Regexp      // nil if not set
	contains    string              // "" if not set
	hasContent  bool                // true if regex OR contains set
	suggestType string
	target      string
	hint        string
}

// NewEngine compiles regex rules eagerly. Returns an error if any
// content_regex fails to compile, so main.go can fail-fast on bad config.
// A rule with no Match fields at all is rejected: it would fire on every
// Run and is almost certainly a config typo.
func NewEngine(cfg Config) (*Engine, error) {
	e := &Engine{
		enabled:     cfg.Enabled,
		injectHint:  cfg.InjectHint,
		defaultHint: cfg.DefaultHint,
	}
	for i, r := range cfg.Rules {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("rule-%d", i)
		}
		cr := compiledRule{
			name:        name,
			suggestType: r.Suggest.Type,
			target:      r.Suggest.Target,
			hint:        r.Suggest.Hint,
			contains:    r.Match.ContentContains,
		}
		if len(r.Match.Profile) > 0 {
			cr.profiles = make(map[string]struct{}, len(r.Match.Profile))
			for _, p := range r.Match.Profile {
				cr.profiles[p] = struct{}{}
			}
		}
		if r.Match.ContentRegex != "" {
			re, err := regexp.Compile(r.Match.ContentRegex)
			if err != nil {
				return nil, fmt.Errorf("orchestrator: rule %q: compile regex %q: %w",
					name, r.Match.ContentRegex, err)
			}
			cr.regex = re
		}
		cr.hasContent = cr.regex != nil || cr.contains != ""
		if !cr.hasContent && len(cr.profiles) == 0 {
			return nil, fmt.Errorf("orchestrator: rule %q has no match predicates", name)
		}
		e.rules = append(e.rules, cr)
	}
	return e, nil
}

// Enabled reports whether the router should run at all. Caller may use this
// to short-circuit audit emission too.
func (e *Engine) Enabled() bool { return e != nil && e.enabled }

// InjectHintEnabled controls whether Route's caller should actually prepend
// Decision.Hint as a system message. Independent of Enabled (a disabled
// engine returns a zero Decision regardless).
func (e *Engine) InjectHintEnabled() bool { return e != nil && e.injectHint }

// Route walks the compiled rule list in declaration order and returns the
// first match. If nothing matches and a default_hint is configured, the
// engine returns a synthetic Decision with RuleName="default". If the engine
// is disabled it returns a zero Decision.
func (e *Engine) Route(_ context.Context, in RouteInput) Decision {
	if !e.Enabled() {
		return Decision{}
	}
	for _, cr := range e.rules {
		if !profileMatches(cr.profiles, in.Profile) {
			continue
		}
		matchedOn, ok := matchContent(cr, in.UserContent)
		if !ok {
			continue
		}
		return Decision{
			Matched:   true,
			RuleName:  cr.name,
			Type:      cr.suggestType,
			Target:    cr.target,
			Hint:      cr.hint,
			MatchedOn: matchedOn,
		}
	}
	if e.defaultHint != "" {
		return Decision{
			Matched:   true,
			RuleName:  "default",
			Hint:      e.defaultHint,
			MatchedOn: "default",
		}
	}
	return Decision{}
}

func profileMatches(allow map[string]struct{}, got string) bool {
	if len(allow) == 0 {
		return true
	}
	_, ok := allow[got]
	return ok
}

// matchContent reports the first content predicate that fired. If the rule
// has no content predicate at all, a profile match alone counts as a hit
// (NewEngine guarantees at least one of profile / content is set).
func matchContent(cr compiledRule, content string) (string, bool) {
	if cr.contains != "" && strings.Contains(content, cr.contains) {
		return "content_contains", true
	}
	if cr.regex != nil && cr.regex.MatchString(content) {
		return "content_regex", true
	}
	if !cr.hasContent {
		return "profile", true
	}
	return "", false
}
