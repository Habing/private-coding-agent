package workflow

import (
	"errors"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Parse decodes a DSL YAML string into a WorkflowDoc and infers each step's
// NodeKind. It rejects YAML that populates multiple node-kind fields on the
// same step (ambiguous intent — would let `use:` + `assign:` silently win).
//
// Parse does NOT validate cross-step references (id uniqueness, expression
// syntax, nesting depth). That's Validate's job; Parse only complains about
// shapes that make subsequent passes meaningless.
func Parse(src string) (*WorkflowDoc, error) {
	var doc WorkflowDoc
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if err := classifySteps(doc.Steps); err != nil {
		return nil, err
	}
	return &doc, nil
}

func classifySteps(steps []Step) error {
	for i := range steps {
		s := &steps[i]
		k, err := classifyStep(s)
		if err != nil {
			id := s.ID
			if id == "" {
				id = fmt.Sprintf("[%d]", i)
			}
			return fmt.Errorf("step %s: %w", id, err)
		}
		s.Kind = k
		if err := classifyChildren(s); err != nil {
			return err
		}
	}
	return nil
}

// classifyStep picks NodeKind off the first non-empty discriminator. It also
// parses the timeout / wait duration strings into native time.Duration so the
// engine doesn't have to ParseDuration on every dispatch.
func classifyStep(s *Step) (NodeKind, error) {
	hits := []NodeKind{}
	if s.Use != "" {
		hits = append(hits, NodeTool)
	}
	if len(s.Assign) > 0 {
		hits = append(hits, NodeAssign)
	}
	if s.If != "" {
		hits = append(hits, NodeIf)
	}
	if s.Foreach != "" {
		hits = append(hits, NodeForeach)
	}
	if len(s.Parallel) > 0 {
		hits = append(hits, NodeParallel)
	}
	if s.Wait != "" {
		hits = append(hits, NodeWait)
	}
	if len(hits) == 0 {
		return "", errors.New("must populate one of: use|assign|if|foreach|parallel|wait")
	}
	if len(hits) > 1 {
		return "", fmt.Errorf("ambiguous: %v populated; pick exactly one", hits)
	}
	k := hits[0]
	if k == NodeTool && s.TimeoutRaw != "" {
		d, err := time.ParseDuration(s.TimeoutRaw)
		if err != nil {
			return "", fmt.Errorf("parse timeout %q: %w", s.TimeoutRaw, err)
		}
		s.Timeout = d
	}
	if k == NodeWait {
		d, err := time.ParseDuration(s.Wait)
		if err != nil {
			return "", fmt.Errorf("parse wait %q: %w", s.Wait, err)
		}
		s.WaitDur = d
	}
	if k == NodeTool && s.OnError == "" {
		s.OnError = OnErrorFail
	}
	if k == NodeTool && s.OnError != OnErrorFail && s.OnError != OnErrorContinue {
		return "", fmt.Errorf("on_error %q: expected fail|continue", s.OnError)
	}
	return k, nil
}

func classifyChildren(s *Step) error {
	switch s.Kind {
	case NodeIf:
		if err := classifySteps(s.Then); err != nil {
			return err
		}
		if err := classifySteps(s.Else); err != nil {
			return err
		}
	case NodeForeach:
		if err := classifySteps(s.Steps); err != nil {
			return err
		}
	case NodeParallel:
		for bi := range s.Parallel {
			if err := classifySteps(s.Parallel[bi]); err != nil {
				return fmt.Errorf("parallel branch %d: %w", bi, err)
			}
		}
	}
	return nil
}
