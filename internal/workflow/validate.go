package workflow

import (
	"fmt"
	"regexp"
)

// slugPattern restricts both WorkflowDoc.ID and the URL slug. Mirrors Skills
// (Slice 17) so admins don't have to learn two conventions.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// stepIDPattern is lighter than slug — step IDs are visible only inside DSL,
// so dots are fine if someone wants step.id namespacing. Forbid empties.
var stepIDPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]*$`)

// Validate inspects a parsed WorkflowDoc and reports the first problem it
// finds: bad slug, duplicate/missing step IDs, ambiguous expression syntax,
// nesting beyond MaxNestingDepth, or parallel fanout beyond MaxParallelFanout.
//
// It does NOT verify that `use:` references a tool that exists in the live
// ToolBus — that's Publish's job (tool set changes per tenant/runtime).
func Validate(doc *WorkflowDoc, cfg Config) error {
	if doc == nil {
		return fmt.Errorf("nil doc")
	}
	if !slugPattern.MatchString(doc.ID) {
		return fmt.Errorf("workflow id %q: must match %s", doc.ID, slugPattern.String())
	}
	if doc.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if len(doc.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}
	seen := map[string]bool{}
	if err := validateSteps(doc.Steps, seen, 0, cfg); err != nil {
		return err
	}
	if err := validateTriggers(doc.Triggers, seen); err != nil {
		return err
	}
	// Outputs are expression templates; require non-empty value.
	for k, v := range doc.Outputs {
		if v == "" {
			return fmt.Errorf("outputs.%s: empty expression", k)
		}
	}
	return nil
}

func validateSteps(steps []Step, seen map[string]bool, depth int, cfg Config) error {
	if depth > cfg.MaxNestingDepth {
		return fmt.Errorf("nesting depth %d exceeds MaxNestingDepth %d", depth, cfg.MaxNestingDepth)
	}
	for i := range steps {
		s := &steps[i]
		if !stepIDPattern.MatchString(s.ID) {
			return fmt.Errorf("step id %q: must match %s", s.ID, stepIDPattern.String())
		}
		if seen[s.ID] {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
		if err := validateStep(s, seen, depth, cfg); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(s *Step, seen map[string]bool, depth int, cfg Config) error {
	switch s.Kind {
	case NodeTool:
		if s.Use == "" {
			return fmt.Errorf("step %s: tool node requires use", s.ID)
		}
	case NodeAssign:
		if len(s.Assign) == 0 {
			return fmt.Errorf("step %s: assign node requires at least one binding", s.ID)
		}
		for k, expr := range s.Assign {
			if k == "" {
				return fmt.Errorf("step %s: assign has empty var name", s.ID)
			}
			if expr == "" {
				return fmt.Errorf("step %s.assign.%s: empty expression", s.ID, k)
			}
		}
	case NodeIf:
		if s.If == "" {
			return fmt.Errorf("step %s: if expression is empty", s.ID)
		}
		if len(s.Then) == 0 {
			return fmt.Errorf("step %s: if requires at least one then-branch step", s.ID)
		}
		if err := validateSteps(s.Then, seen, depth+1, cfg); err != nil {
			return err
		}
		if len(s.Else) > 0 {
			if err := validateSteps(s.Else, seen, depth+1, cfg); err != nil {
				return err
			}
		}
	case NodeForeach:
		if s.Foreach == "" {
			return fmt.Errorf("step %s: foreach expression is empty", s.ID)
		}
		if s.As == "" {
			return fmt.Errorf("step %s: foreach requires `as` binding name", s.ID)
		}
		if len(s.Steps) == 0 {
			return fmt.Errorf("step %s: foreach requires at least one inner step", s.ID)
		}
		if err := validateSteps(s.Steps, seen, depth+1, cfg); err != nil {
			return err
		}
	case NodeParallel:
		if len(s.Parallel) < 2 {
			return fmt.Errorf("step %s: parallel requires at least 2 branches", s.ID)
		}
		if len(s.Parallel) > cfg.MaxParallelFanout {
			return fmt.Errorf("step %s: parallel fanout %d > MaxParallelFanout %d",
				s.ID, len(s.Parallel), cfg.MaxParallelFanout)
		}
		for bi, branch := range s.Parallel {
			if len(branch) == 0 {
				return fmt.Errorf("step %s: parallel branch %d is empty", s.ID, bi)
			}
			if err := validateSteps(branch, seen, depth+1, cfg); err != nil {
				return err
			}
		}
	case NodeWait:
		if s.WaitDur <= 0 {
			return fmt.Errorf("step %s: wait duration must be positive", s.ID)
		}
	default:
		return fmt.Errorf("step %s: unknown kind %q", s.ID, s.Kind)
	}
	return nil
}
