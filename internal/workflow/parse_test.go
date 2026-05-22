package workflow

import (
	"strings"
	"testing"
	"time"
)

func TestParse_HelloMinimal(t *testing.T) {
	src := `
id: hello
name: Hello
steps:
  - id: greet
    assign:
      who: ${inputs.name}
outputs:
  msg: hello ${vars.who}
`
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.ID != "hello" || doc.Name != "Hello" {
		t.Fatalf("doc header: %+v", doc)
	}
	if len(doc.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(doc.Steps))
	}
	if doc.Steps[0].Kind != NodeAssign {
		t.Fatalf("expected assign kind, got %q", doc.Steps[0].Kind)
	}
	if doc.Outputs["msg"] != "hello ${vars.who}" {
		t.Fatalf("outputs.msg: %q", doc.Outputs["msg"])
	}
}

func TestParse_AllFiveKinds(t *testing.T) {
	src := `
id: kitchen-sink
name: Kitchen Sink
steps:
  - id: t
    use: shell.exec
    args: { command: "echo hi" }
    timeout: 5s
    on_error: continue
  - id: a
    assign:
      x: ${steps.t.output.exit_code}
  - id: cond
    if: ${vars.x == 0}
    then:
      - id: inner1
        wait: 10ms
    else:
      - id: inner2
        wait: 20ms
  - id: loop
    foreach: ${inputs.items}
    as: it
    steps:
      - id: loop_inner
        assign:
          y: ${vars.it}
  - id: par
    parallel:
      - - id: b1
          wait: 1ms
      - - id: b2
          wait: 2ms
  - id: pause
    wait: 100ms
`
	doc, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	kinds := []NodeKind{}
	for _, s := range doc.Steps {
		kinds = append(kinds, s.Kind)
	}
	want := []NodeKind{NodeTool, NodeAssign, NodeIf, NodeForeach, NodeParallel, NodeWait}
	for i, k := range want {
		if kinds[i] != k {
			t.Fatalf("step %d kind=%q want=%q", i, kinds[i], k)
		}
	}
	// tool durations parsed
	if doc.Steps[0].Timeout != 5*time.Second {
		t.Fatalf("timeout: %v", doc.Steps[0].Timeout)
	}
	if doc.Steps[0].OnError != "continue" {
		t.Fatalf("on_error: %v", doc.Steps[0].OnError)
	}
	// wait duration parsed
	if doc.Steps[5].WaitDur != 100*time.Millisecond {
		t.Fatalf("wait: %v", doc.Steps[5].WaitDur)
	}
	// nested classification
	if doc.Steps[2].Then[0].Kind != NodeWait || doc.Steps[2].Else[0].Kind != NodeWait {
		t.Fatalf("if branches not classified")
	}
	if doc.Steps[3].Steps[0].Kind != NodeAssign {
		t.Fatalf("foreach inner not classified")
	}
	if doc.Steps[4].Parallel[0][0].Kind != NodeWait {
		t.Fatalf("parallel branch not classified")
	}
}

func TestParse_AmbiguousStep(t *testing.T) {
	src := `
id: bad
name: Bad
steps:
  - id: oops
    use: shell.exec
    assign:
      x: 1
`
	_, err := Parse(src)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got %v", err)
	}
}

func TestParse_EmptyStep(t *testing.T) {
	src := `
id: bad
name: Bad
steps:
  - id: nothing
`
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected error for step with no kind")
	}
}

func TestParse_BadTimeout(t *testing.T) {
	src := `
id: bad
name: Bad
steps:
  - id: t
    use: shell.exec
    timeout: forever
`
	_, err := Parse(src)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout parse error, got %v", err)
	}
}

func TestParse_BadOnError(t *testing.T) {
	src := `
id: bad
name: Bad
steps:
  - id: t
    use: shell.exec
    on_error: rollback
`
	_, err := Parse(src)
	if err == nil || !strings.Contains(err.Error(), "on_error") {
		t.Fatalf("expected on_error error, got %v", err)
	}
}

func TestParse_BadWaitDuration(t *testing.T) {
	src := `
id: bad
name: Bad
steps:
  - id: w
    wait: nope
`
	_, err := Parse(src)
	if err == nil || !strings.Contains(err.Error(), "wait") {
		t.Fatalf("expected wait parse error, got %v", err)
	}
}
