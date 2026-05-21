package skills

import "log/slog"

// ResolveInput is what the agent engine feeds to the resolver each Run.
// First non-empty slice wins (Run > Session > Profile > config default).
type ResolveInput struct {
	RunSkillIDs     []string
	SessionSkillIDs []string
	ProfileSkillIDs []string
}

// Resolver picks skills out of the Registry according to the ADR-67
// priority chain. Missing ids are skipped with a warn log (not an error).
type Resolver struct {
	registry *Registry
	cfg      Config
}

func NewResolver(registry *Registry, cfg Config) *Resolver {
	return &Resolver{registry: registry, cfg: cfg}
}

// Resolve returns ordered, deduplicated, registry-resident skills, capped
// at cfg.MaxSkillsPerRun.
func (r *Resolver) Resolve(in ResolveInput) []*Skill {
	if r == nil || !r.cfg.Enabled || r.registry == nil {
		return nil
	}
	ids := firstNonEmpty(in.RunSkillIDs, in.SessionSkillIDs, in.ProfileSkillIDs, r.cfg.DefaultSkillIDs)
	if len(ids) == 0 {
		return nil
	}
	max := r.cfg.MaxSkillsPerRun
	if max <= 0 {
		max = 5
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]*Skill, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		sk, ok := r.registry.Get(id)
		if !ok {
			slog.Warn("skills.resolve_unknown_id", "id", id)
			continue
		}
		out = append(out, sk)
		if len(out) >= max {
			break
		}
	}
	return out
}

func firstNonEmpty(lists ...[]string) []string {
	for _, l := range lists {
		if len(l) > 0 {
			return l
		}
	}
	return nil
}
