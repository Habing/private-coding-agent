package skills

import (
	"strings"
	"testing"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func skill(id, desc, body string) *Skill {
	return &Skill{
		Document:  Document{ID: id, Description: desc, Body: body},
		Version:   "v",
		CharCount: len(body),
	}
}

func TestInjector_EmptyEverything(t *testing.T) {
	r := BuildSystemMessages("", nil, 1000)
	if len(r.Messages) != 0 {
		t.Fatalf("expected no messages, got %v", r.Messages)
	}
	if r.Truncated {
		t.Fatal("should not truncate when there's nothing")
	}
}

func TestInjector_ProfileOnly(t *testing.T) {
	r := BuildSystemMessages("you are helpful", nil, 1000)
	if len(r.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(r.Messages))
	}
	if r.Messages[0].Role != modelgw.RoleSystem {
		t.Fatalf("role = %q", r.Messages[0].Role)
	}
	if r.Messages[0].Content != "you are helpful" {
		t.Fatalf("content = %q", r.Messages[0].Content)
	}
	if len(r.SkillIDs) != 0 {
		t.Fatalf("skillIDs should be empty: %v", r.SkillIDs)
	}
}

func TestInjector_ProfileAndSkills(t *testing.T) {
	skills := []*Skill{
		skill("alpha", "first skill", "rules for alpha"),
		skill("beta", "second skill", "rules for beta"),
	}
	r := BuildSystemMessages("base prompt", skills, 1000)
	if len(r.Messages) != 1 {
		t.Fatalf("expected 1 merged system msg, got %d", len(r.Messages))
	}
	got := r.Messages[0].Content
	if !strings.Contains(got, "base prompt") {
		t.Fatal("profile prompt missing")
	}
	if !strings.Contains(got, "## Active Skills") {
		t.Fatal("Active Skills header missing")
	}
	if !strings.Contains(got, "### Skill: alpha") || !strings.Contains(got, "### Skill: beta") {
		t.Fatalf("skill headers missing in: %s", got)
	}
	if !strings.Contains(got, "rules for alpha") || !strings.Contains(got, "rules for beta") {
		t.Fatalf("skill body missing in: %s", got)
	}
	if r.Truncated {
		t.Fatal("should not truncate")
	}
	if len(r.SkillIDs) != 2 || r.SkillIDs[0] != "alpha" || r.SkillIDs[1] != "beta" {
		t.Fatalf("skill IDs order wrong: %v", r.SkillIDs)
	}
}

func TestInjector_SkillsOnly(t *testing.T) {
	skills := []*Skill{skill("solo", "desc", "body")}
	r := BuildSystemMessages("", skills, 1000)
	if len(r.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(r.Messages))
	}
	if !strings.Contains(r.Messages[0].Content, "## Active Skills") {
		t.Fatal("header missing")
	}
}

func TestInjector_TruncationOnBudget(t *testing.T) {
	long := strings.Repeat("x", 500)
	skills := []*Skill{
		skill("a", "first", long),
		skill("b", "second", long),
		skill("c", "third", long),
	}
	r := BuildSystemMessages("prefix", skills, 300)
	if !r.Truncated {
		t.Fatal("expected truncation")
	}
	if len(r.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(r.Messages))
	}
	if got := len(r.Messages[0].Content); got > 320 {
		t.Fatalf("content too long: %d", got)
	}
	if len(r.SkillIDs) >= 3 {
		t.Fatalf("third skill should not have been included: %v", r.SkillIDs)
	}
}

func TestInjector_SkillsPreserveInputOrder(t *testing.T) {
	skills := []*Skill{
		skill("zulu", "", "Z"),
		skill("alpha", "", "A"),
		skill("mike", "", "M"),
	}
	r := BuildSystemMessages("", skills, 10000)
	got := r.Messages[0].Content
	zi := strings.Index(got, "### Skill: zulu")
	ai := strings.Index(got, "### Skill: alpha")
	mi := strings.Index(got, "### Skill: mike")
	if !(zi < ai && ai < mi) {
		t.Fatalf("order broken: zulu=%d alpha=%d mike=%d", zi, ai, mi)
	}
}
