package workflow

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *WorkflowDoc {
	t.Helper()
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

func TestValidate_HappyPath(t *testing.T) {
	src := `
id: ok-flow
name: OK
steps:
  - id: a
    use: shell.exec
    args: { command: "echo a" }
  - id: b
    assign:
      x: ${steps.a.output.exit_code}
outputs:
  x: ${vars.x}
`
	if err := Validate(mustParse(t, src), DefaultConfig()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	src := `
id: dup
name: D
steps:
  - id: a
    wait: 1ms
  - id: a
    wait: 2ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate id, got %v", err)
	}
}

func TestValidate_DuplicateIDAcrossNesting(t *testing.T) {
	src := `
id: dupx
name: D
steps:
  - id: outer
    if: ${inputs.x}
    then:
      - id: dup
        wait: 1ms
    else:
      - id: dup
        wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate across nesting, got %v", err)
	}
}

func TestValidate_BadSlug(t *testing.T) {
	src := `
id: NotKebab
name: X
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "workflow id") {
		t.Fatalf("expected slug error, got %v", err)
	}
}

func TestValidate_NestingDepthCap(t *testing.T) {
	// Build a deeply nested if to exceed depth=2 (use a small cfg for cheap test).
	src := `
id: deep
name: D
steps:
  - id: lvl1
    if: ${a}
    then:
      - id: lvl2
        if: ${b}
        then:
          - id: lvl3
            wait: 1ms
`
	cfg := DefaultConfig()
	cfg.MaxNestingDepth = 1
	err := Validate(mustParse(t, src), cfg)
	if err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("expected nesting cap, got %v", err)
	}
}

func TestValidate_ParallelTooFewBranches(t *testing.T) {
	src := `
id: p
name: P
steps:
  - id: par
    parallel:
      - - id: only
          wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "parallel") {
		t.Fatalf("expected parallel branch error, got %v", err)
	}
}

func TestValidate_ParallelFanoutCap(t *testing.T) {
	branches := ""
	for i := 0; i < 10; i++ {
		branches += "      - - id: b" + string(rune('a'+i)) + "\n          wait: 1ms\n"
	}
	src := "id: fanout\nname: F\nsteps:\n  - id: par\n    parallel:\n" + branches
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "fanout") {
		t.Fatalf("expected fanout cap error, got %v", err)
	}
}

func TestValidate_EmptyAssign(t *testing.T) {
	src := `
id: a
name: A
steps:
  - id: bad
    assign: {}
`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse-time error for empty assign (no discriminator populated)")
	}
}

func TestValidate_IfMissingThenBranch(t *testing.T) {
	src := `
id: if-noop
name: X
steps:
  - id: bad
    if: ${vars.x}
`
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := Validate(doc, DefaultConfig()); err == nil {
		t.Fatal("expected validate error when if has no then-branch")
	}
}
