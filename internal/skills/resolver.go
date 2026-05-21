package skills

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// ResolveInput is what the agent engine feeds to the resolver each Run.
// First non-empty slice wins (Run > Session > Profile > config default).
type ResolveInput struct {
	RunSkillIDs     []string
	SessionSkillIDs []string
	ProfileSkillIDs []string
}

// DBLookup is the subset of *DBRepo the Resolver needs. Declared as an
// interface so unit tests can stub without a real database. Nil-ok: callers
// without a backing DB pass nil and the resolver degrades to FS-only.
type DBLookup interface {
	ListEnabled(ctx context.Context, tenantID uuid.UUID) ([]DBSkill, error)
	GetForProfile(ctx context.Context, tenantID uuid.UUID, profile string) ([]string, error)
}

// Resolver picks skills out of the Registry (filesystem) and an optional
// DBLookup (tenant scope) according to the ADR-67 priority chain. When a
// tenant DB row shares a key with a filesystem skill, the DB row wins —
// that's how a tenant overrides a platform default.
type Resolver struct {
	registry *Registry
	db       DBLookup
	cfg      Config
}

func NewResolver(registry *Registry, cfg Config) *Resolver {
	return &Resolver{registry: registry, cfg: cfg}
}

// WithDBLookup attaches the tenant-scoped Skills DB. nil is a valid argument
// (resolver stays FS-only).
func (r *Resolver) WithDBLookup(db DBLookup) *Resolver {
	if r != nil {
		r.db = db
	}
	return r
}

// Resolve is the FS-only entry point retained for tests and callers that have
// no tenant context. Equivalent to ResolveForTenant with tenantID=uuid.Nil
// and an empty profileName.
func (r *Resolver) Resolve(in ResolveInput) []*Skill {
	return r.ResolveForTenant(context.Background(), uuid.Nil, "", in)
}

// ResolveForTenant returns ordered, deduplicated skills for a (tenant, profile)
// pair. DB rows for the tenant shadow filesystem entries with the same key;
// the tenant_profile_skills binding (if any) overrides Profile.SkillIDs.
func (r *Resolver) ResolveForTenant(ctx context.Context, tenantID uuid.UUID,
	profileName string, in ResolveInput) []*Skill {
	if r == nil || !r.cfg.Enabled {
		return nil
	}
	if r.registry == nil && r.db == nil {
		return nil
	}

	// Load tenant DB skills + profile binding once per resolve. Both are
	// optional: errors are logged and treated as "no override".
	var dbByKey map[string]*Skill
	var dbProfileBinding []string
	if r.db != nil && tenantID != uuid.Nil {
		if rows, err := r.db.ListEnabled(ctx, tenantID); err != nil {
			slog.Warn("skills.db.list_enabled", "tenant_id", tenantID.String(), "err", err.Error())
		} else if len(rows) > 0 {
			dbByKey = make(map[string]*Skill, len(rows))
			for i := range rows {
				dbByKey[rows[i].SkillKey] = rows[i].ToSkill()
			}
		}
		if profileName != "" {
			if bind, err := r.db.GetForProfile(ctx, tenantID, profileName); err != nil {
				slog.Warn("skills.db.profile_binding", "tenant_id", tenantID.String(),
					"profile", profileName, "err", err.Error())
			} else if len(bind) > 0 {
				dbProfileBinding = bind
			}
		}
	}

	profileIDs := in.ProfileSkillIDs
	if len(dbProfileBinding) > 0 {
		profileIDs = dbProfileBinding
	}
	ids := firstNonEmpty(in.RunSkillIDs, in.SessionSkillIDs, profileIDs, r.cfg.DefaultSkillIDs)
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
		// DB wins over FS on duplicate key. When neither has it, log + skip.
		if sk, ok := dbByKey[id]; ok {
			out = append(out, sk)
		} else if sk, ok := r.lookupRegistry(id); ok {
			out = append(out, sk)
		} else {
			slog.Warn("skills.resolve_unknown_id", "id", id, "tenant_id", tenantID.String())
			continue
		}
		if len(out) >= max {
			break
		}
	}
	return out
}

func (r *Resolver) lookupRegistry(id string) (*Skill, bool) {
	if r.registry == nil {
		return nil, false
	}
	return r.registry.Get(id)
}

func firstNonEmpty(lists ...[]string) []string {
	for _, l := range lists {
		if len(l) > 0 {
			return l
		}
	}
	return nil
}
