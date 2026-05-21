package skills

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// stubDB is an in-memory DBLookup used by tests that exercise tenant override
// behavior without a real database.
type stubDB struct {
	enabled        map[uuid.UUID][]DBSkill
	profileBinding map[string][]string // key: "<tenant>/<profile>"
	err            error
}

func (s *stubDB) ListEnabled(_ context.Context, t uuid.UUID) ([]DBSkill, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.enabled[t], nil
}
func (s *stubDB) GetForProfile(_ context.Context, t uuid.UUID, p string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.profileBinding[t.String()+"/"+p], nil
}

func mkRegistry(t *testing.T, ids ...string) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, id := range ids {
		r.byID[id] = &Skill{Document: Document{ID: id, Body: "body of " + id}, Version: "v", CharCount: len("body of " + id)}
	}
	return r
}

func TestResolver_PriorityRun(t *testing.T) {
	reg := mkRegistry(t, "a", "b", "c", "d")
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5})
	got := r.Resolve(ResolveInput{
		RunSkillIDs:     []string{"a"},
		SessionSkillIDs: []string{"b"},
		ProfileSkillIDs: []string{"c"},
	})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("run should win: %+v", got)
	}
}

func TestResolver_FallbackSessionThenProfileThenDefault(t *testing.T) {
	reg := mkRegistry(t, "a", "b", "c", "d")
	cfg := Config{Enabled: true, MaxSkillsPerRun: 5, DefaultSkillIDs: []string{"d"}}
	r := NewResolver(reg, cfg)
	// session wins
	got := r.Resolve(ResolveInput{SessionSkillIDs: []string{"b"}, ProfileSkillIDs: []string{"c"}})
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("session expected: %+v", got)
	}
	// profile wins
	got = r.Resolve(ResolveInput{ProfileSkillIDs: []string{"c"}})
	if len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("profile expected: %+v", got)
	}
	// default wins
	got = r.Resolve(ResolveInput{})
	if len(got) != 1 || got[0].ID != "d" {
		t.Fatalf("default expected: %+v", got)
	}
}

func TestResolver_UnknownIDsSkipped(t *testing.T) {
	reg := mkRegistry(t, "a")
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5})
	got := r.Resolve(ResolveInput{RunSkillIDs: []string{"missing", "a", "ghost"}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("only a should resolve: %+v", got)
	}
}

func TestResolver_DedupPreservesOrder(t *testing.T) {
	reg := mkRegistry(t, "a", "b")
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5})
	got := r.Resolve(ResolveInput{RunSkillIDs: []string{"a", "b", "a", "b"}})
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("dedup order broken: %+v", got)
	}
}

func TestResolver_MaxSkillsCap(t *testing.T) {
	reg := mkRegistry(t, "a", "b", "c", "d", "e", "f")
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 3})
	got := r.Resolve(ResolveInput{RunSkillIDs: []string{"a", "b", "c", "d", "e", "f"}})
	if len(got) != 3 {
		t.Fatalf("expected cap to 3, got %d", len(got))
	}
}

func TestResolver_DisabledReturnsNil(t *testing.T) {
	reg := mkRegistry(t, "a")
	r := NewResolver(reg, Config{Enabled: false, MaxSkillsPerRun: 5})
	if got := r.Resolve(ResolveInput{RunSkillIDs: []string{"a"}}); got != nil {
		t.Fatalf("disabled should return nil, got %+v", got)
	}
}

func TestResolver_NilRegistry(t *testing.T) {
	r := NewResolver(nil, Config{Enabled: true, MaxSkillsPerRun: 5})
	if got := r.Resolve(ResolveInput{RunSkillIDs: []string{"a"}}); got != nil {
		t.Fatalf("nil registry should return nil")
	}
}

func TestResolver_DBSkillShadowsFS(t *testing.T) {
	reg := mkRegistry(t, "platform-coding-standards")
	tenant := uuid.New()
	db := &stubDB{enabled: map[uuid.UUID][]DBSkill{
		tenant: {{
			SkillKey: "platform-coding-standards",
			Body:     "TENANT-OVERRIDE-BODY",
			Enabled:  true,
		}},
	}}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	got := r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{RunSkillIDs: []string{"platform-coding-standards"}})
	if len(got) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(got))
	}
	if got[0].Body != "TENANT-OVERRIDE-BODY" {
		t.Fatalf("DB should shadow FS; got body=%q", got[0].Body)
	}
}

func TestResolver_DBSkillAddedAlongsideFS(t *testing.T) {
	reg := mkRegistry(t, "platform-coding-standards")
	tenant := uuid.New()
	db := &stubDB{enabled: map[uuid.UUID][]DBSkill{
		tenant: {{SkillKey: "tenant-marker", Body: "MARKER", Enabled: true}},
	}}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	got := r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{RunSkillIDs: []string{"tenant-marker", "platform-coding-standards"}})
	if len(got) != 2 || got[0].ID != "tenant-marker" || got[1].ID != "platform-coding-standards" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestResolver_DBProfileBindingOverridesProfileSkillIDs(t *testing.T) {
	reg := mkRegistry(t, "a", "b", "c")
	tenant := uuid.New()
	db := &stubDB{
		enabled: map[uuid.UUID][]DBSkill{},
		profileBinding: map[string][]string{
			tenant.String() + "/coding": {"b", "c"},
		},
	}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	got := r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{ProfileSkillIDs: []string{"a"}})
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "c" {
		t.Fatalf("expected DB binding [b,c], got %+v", got)
	}
}

func TestResolver_DBProfileBindingIgnoredWhenRunOrSessionPresent(t *testing.T) {
	reg := mkRegistry(t, "a", "b", "c")
	tenant := uuid.New()
	db := &stubDB{
		enabled: map[uuid.UUID][]DBSkill{},
		profileBinding: map[string][]string{
			tenant.String() + "/coding": {"b"},
		},
	}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	// run > profile binding
	got := r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{RunSkillIDs: []string{"a"}, ProfileSkillIDs: []string{"c"}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("Run should win: %+v", got)
	}
	// session > profile binding
	got = r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{SessionSkillIDs: []string{"a"}, ProfileSkillIDs: []string{"c"}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("Session should win: %+v", got)
	}
}

func TestResolver_DBLookupErrorFallsBackToFS(t *testing.T) {
	reg := mkRegistry(t, "a")
	tenant := uuid.New()
	db := &stubDB{err: errors.New("boom")}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	got := r.ResolveForTenant(context.Background(), tenant, "coding",
		ResolveInput{RunSkillIDs: []string{"a"}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("expected FS fallback, got %+v", got)
	}
}

func TestResolver_TenantNilSkipsDBLookup(t *testing.T) {
	reg := mkRegistry(t, "a")
	// stub would panic if iterated; tenant=uuid.Nil should skip it
	db := &stubDB{}
	r := NewResolver(reg, Config{Enabled: true, MaxSkillsPerRun: 5}).WithDBLookup(db)
	// Make sure deadline-bound ctx doesn't kill us
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := r.ResolveForTenant(ctx, uuid.Nil, "coding", ResolveInput{RunSkillIDs: []string{"a"}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("expected a, got %+v", got)
	}
}
