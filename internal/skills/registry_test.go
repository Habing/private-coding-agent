package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func mkSkill(t *testing.T, dir, sub, name, body string) {
	t.Helper()
	full := filepath.Join(dir, sub)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", full, err)
	}
	content := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

func TestRegistry_LoadFromDirs_Basic(t *testing.T) {
	dir := t.TempDir()
	mkSkill(t, dir, "platform/coding", "platform-coding", "rule A")
	mkSkill(t, dir, "e2e/marker", "e2e-marker", "echo MARKER")
	// noise: non-SKILL.md file should be skipped
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := NewRegistry()
	n, errs := r.LoadFromDirs([]string{dir})
	if len(errs) != 0 {
		t.Fatalf("unexpected errs: %v", errs)
	}
	if n != 2 {
		t.Fatalf("loaded = %d, want 2", n)
	}
	if _, ok := r.Get("platform-coding"); !ok {
		t.Fatalf("platform-coding missing")
	}
	if _, ok := r.Get("e2e-marker"); !ok {
		t.Fatalf("e2e-marker missing")
	}
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("list len = %d", len(list))
	}
	// sorted alphabetically: e2e-marker < platform-coding
	if list[0].ID != "e2e-marker" || list[1].ID != "platform-coding" {
		t.Fatalf("not sorted: %+v", list)
	}
	// no body in meta — sanity check: version is 12 chars hex
	if len(list[0].Version) != 12 {
		t.Fatalf("version len = %d", len(list[0].Version))
	}
}

func TestRegistry_DuplicateID_LaterWins(t *testing.T) {
	dir := t.TempDir()
	mkSkill(t, dir, "a", "dup", "first")
	mkSkill(t, dir, "b", "dup", "second")
	r := NewRegistry()
	n, _ := r.LoadFromDirs([]string{dir})
	if n != 2 {
		t.Fatalf("loaded = %d", n)
	}
	sk, ok := r.Get("dup")
	if !ok {
		t.Fatal("dup missing")
	}
	// later (b) should win — but walk order is alphabetical so b > a => b wins
	if sk.Body != "second" {
		t.Fatalf("body = %q, want second", sk.Body)
	}
}

func TestRegistry_NonexistentDir_ReportedNotPanicking(t *testing.T) {
	r := NewRegistry()
	_, errs := r.LoadFromDirs([]string{filepath.Join(t.TempDir(), "does-not-exist")})
	if len(errs) == 0 {
		t.Fatal("expected error for missing dir")
	}
}

func TestRegistry_ParseError_CollectedNotFatal(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "SKILL.md"), []byte("no frontmatter here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mkSkill(t, dir, "ok", "ok-skill", "fine")
	r := NewRegistry()
	n, errs := r.LoadFromDirs([]string{dir})
	if n != 1 {
		t.Fatalf("loaded = %d, want 1", n)
	}
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
}

func TestRegistry_NilGetSafe(t *testing.T) {
	var r *Registry
	if _, ok := r.Get("anything"); ok {
		t.Fatal("nil registry should not return ok")
	}
	if list := r.List(); list != nil {
		t.Fatalf("nil registry List should be nil, got %v", list)
	}
}
