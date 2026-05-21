package agent_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/skills"
)

func TestNoopComposer_ProfileOnly(t *testing.T) {
	msgs, meta, err := agent.NoopComposer{}.ComposeSystem(context.Background(), agent.ComposeInput{
		Profile: agent.Profile{SystemPrompt: "hi"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != modelgw.RoleSystem || msgs[0].Content != "hi" {
		t.Fatalf("unexpected msgs: %+v", msgs)
	}
	if len(meta.SkillIDs) != 0 || meta.CharCount != 0 {
		t.Fatalf("meta should be zero: %+v", meta)
	}
}

func TestNoopComposer_EmptyProfile(t *testing.T) {
	msgs, _, err := agent.NoopComposer{}.ComposeSystem(context.Background(), agent.ComposeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected no msgs, got %+v", msgs)
	}
}

func TestSkillComposer_ProfileSkillIDs_Fallback(t *testing.T) {
	reg := skills.NewRegistry()
	// inject one skill via temp dir for realism
	td := t.TempDir()
	mkSkillFile(t, td, "useful")
	if n, errs := reg.LoadFromDirs([]string{td}); n != 1 || len(errs) > 0 {
		t.Fatalf("loaded=%d errs=%v", n, errs)
	}
	cfg := skills.Config{Enabled: true, MaxInjectedChars: 10000, MaxSkillsPerRun: 5}
	c := agent.NewSkillComposer(skills.NewResolver(reg, cfg), cfg)
	msgs, meta, err := c.ComposeSystem(context.Background(), agent.ComposeInput{
		Profile: agent.Profile{SystemPrompt: "base", SkillIDs: []string{"useful"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %d", len(msgs))
	}
	got := msgs[0].Content
	if !strings.Contains(got, "base") || !strings.Contains(got, "### Skill: useful") {
		t.Fatalf("content missing parts: %s", got)
	}
	if len(meta.SkillIDs) != 1 || meta.SkillIDs[0] != "useful" {
		t.Fatalf("meta.SkillIDs = %v", meta.SkillIDs)
	}
}

func TestSkillComposer_RunOverridesSession(t *testing.T) {
	reg := skills.NewRegistry()
	td := t.TempDir()
	mkSkillFile(t, td, "alpha")
	mkSkillFile(t, td, "beta")
	if n, _ := reg.LoadFromDirs([]string{td}); n != 2 {
		t.Fatalf("loaded=%d", n)
	}
	cfg := skills.Config{Enabled: true, MaxInjectedChars: 10000, MaxSkillsPerRun: 5}
	c := agent.NewSkillComposer(skills.NewResolver(reg, cfg), cfg)
	_, meta, err := c.ComposeSystem(context.Background(), agent.ComposeInput{
		Profile:         agent.Profile{SystemPrompt: "p", SkillIDs: []string{"beta"}},
		SessionSkillIDs: []string{"beta"},
		RunSkillIDs:     []string{"alpha"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.SkillIDs) != 1 || meta.SkillIDs[0] != "alpha" {
		t.Fatalf("run override failed: %v", meta.SkillIDs)
	}
}

func TestSkillComposer_DisabledNoop(t *testing.T) {
	cfg := skills.Config{Enabled: false, MaxInjectedChars: 1000, MaxSkillsPerRun: 5}
	c := agent.NewSkillComposer(skills.NewResolver(skills.NewRegistry(), cfg), cfg)
	msgs, meta, err := c.ComposeSystem(context.Background(), agent.ComposeInput{
		Profile: agent.Profile{SystemPrompt: "base", SkillIDs: []string{"useful"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.SkillIDs) != 0 {
		t.Fatalf("disabled should resolve to no skills, got %v", meta.SkillIDs)
	}
	if len(msgs) != 1 || msgs[0].Content != "base" {
		t.Fatalf("expected profile-only msg, got %+v", msgs)
	}
}

func mkSkillFile(t *testing.T, dir, name string) {
	t.Helper()
	sub := filepath.Join(dir, name)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nname: " + name + "\ndescription: " + name + " skill\n---\n\nbody of " + name + "\n"
	if err := os.WriteFile(filepath.Join(sub, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}
