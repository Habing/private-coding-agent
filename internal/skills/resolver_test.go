package skills

import "testing"

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
