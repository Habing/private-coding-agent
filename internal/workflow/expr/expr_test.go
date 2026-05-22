package expr

import (
	"testing"
)

func scope() Scope {
	return Scope{
		Inputs: map[string]any{
			"name":  "alice",
			"count": 3,
			"flag":  true,
			"list":  []any{"a", "b", "c"},
		},
		Vars: map[string]any{
			"x":  10,
			"y":  0,
			"ok": true,
		},
		Steps: map[string]StepResult{
			"lint": {Output: map[string]any{"exit_code": 0, "stderr": ""}},
			"test": {Output: map[string]any{"exit_code": 2, "stderr": "two failed"}},
			"err":  {Error: "boom"},
		},
	}
}

func TestResolve_SingleExprPreservesType(t *testing.T) {
	v, err := Resolve("${inputs.count}", scope())
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := v.(int); !ok || n != 3 {
		t.Fatalf("expected int 3, got %T %v", v, v)
	}
}

func TestResolve_MultiSegmentConcatString(t *testing.T) {
	v, err := Resolve("hello ${inputs.name} (count=${inputs.count})", scope())
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(string)
	if !ok || s != "hello alice (count=3)" {
		t.Fatalf("got %T %q", v, v)
	}
}

func TestResolve_NoSubstitutionsReturnsString(t *testing.T) {
	v, err := Resolve("plain string", scope())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.(string); !ok || s != "plain string" {
		t.Fatalf("got %T %v", v, v)
	}
}

func TestResolve_NestedPath(t *testing.T) {
	v, err := Resolve("${steps.lint.output.exit_code}", scope())
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := v.(int); !ok || n != 0 {
		t.Fatalf("got %T %v", v, v)
	}
}

func TestResolve_StepError(t *testing.T) {
	v, err := Resolve("${steps.err.error}", scope())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.(string); !ok || s != "boom" {
		t.Fatalf("got %T %v", v, v)
	}
}

func TestEvalBool_Truthy(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"vars.ok", true},
		{"vars.x", true},
		{"vars.y", false},
		{"inputs.flag", true},
	}
	for _, c := range cases {
		got, err := EvalBool(c.expr, scope())
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s = %v want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalBool_Equality(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"steps.lint.output.exit_code == 0", true},
		{"steps.test.output.exit_code == 0", false},
		{"steps.lint.output.exit_code != 0", false},
		{`inputs.name == "alice"`, true},
		{`inputs.name == "bob"`, false},
		{"inputs.flag == true", true},
	}
	for _, c := range cases {
		got, err := EvalBool(c.expr, scope())
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s = %v want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalBool_NumericCompare(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"vars.x > 5", true},
		{"vars.x < 5", false},
		{"vars.x >= 10", true},
		{"vars.x <= 10", true},
		{"vars.y < 1", true},
	}
	for _, c := range cases {
		got, err := EvalBool(c.expr, scope())
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s = %v want %v", c.expr, got, c.want)
		}
	}
}

func TestEvalBool_LogicalShortCircuit(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"vars.ok && vars.x > 5", true},
		{"vars.y > 0 && vars.x > 0", false}, // short-circuit on first
		{"vars.y > 0 || vars.x > 0", true},
		{"vars.y > 0 || vars.y < 0", false},
		{"! vars.y", true},
		{"! vars.ok", false},
	}
	for _, c := range cases {
		got, err := EvalBool(c.expr, scope())
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("%s = %v want %v", c.expr, got, c.want)
		}
	}
}

func TestResolve_UnknownPathErrors(t *testing.T) {
	_, err := Resolve("${vars.nope}", scope())
	// vars.nope is allowed (returns nil from map lookup), Resolve should succeed with nil.
	if err != nil {
		t.Fatalf("expected nil result for missing var, got err: %v", err)
	}
	_, err = Resolve("${ghost.foo}", scope())
	if err == nil {
		t.Fatal("expected error for unknown root")
	}
}

func TestResolve_ArrayIndex(t *testing.T) {
	v, err := Resolve("${inputs.list.1}", scope())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := v.(string); !ok || s != "b" {
		t.Fatalf("got %T %v", v, v)
	}
}

func TestResolve_UnterminatedExpr(t *testing.T) {
	_, err := Resolve("hello ${broken", scope())
	if err == nil {
		t.Fatal("expected unterminated error")
	}
}
