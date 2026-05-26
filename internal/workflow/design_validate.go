package workflow

import "fmt"

// ValidateDesign checks design shape and optional tool allowlist.
func ValidateDesign(d *WorkflowDesign, allowedTools map[string]struct{}) error {
	if d == nil {
		return fmt.Errorf("design: nil")
	}
	if d.ID == "" {
		return fmt.Errorf("design: id required")
	}
	if d.Name == "" {
		return fmt.Errorf("design: name required")
	}
	if len(d.Steps) == 0 {
		return fmt.Errorf("design: at least one step required")
	}
	seen := map[string]struct{}{}
	var walk func(steps []DesignStep) error
	walk = func(steps []DesignStep) error {
		for _, s := range steps {
			if s.ID == "" {
				return fmt.Errorf("design: step id required")
			}
			if _, ok := seen[s.ID]; ok {
				return fmt.Errorf("design: duplicate step id %q", s.ID)
			}
			seen[s.ID] = struct{}{}
			if s.Kind == "tool" || s.Kind == "" {
				if s.Tool == "" {
					return fmt.Errorf("step %s: tool required", s.ID)
				}
				if allowedTools != nil {
					if _, ok := allowedTools[s.Tool]; !ok {
						return fmt.Errorf("step %s: tool %q not registered", s.ID, s.Tool)
					}
				}
			}
			if s.Kind == "assign" {
				if len(s.Assignments) == 0 {
					return fmt.Errorf("step %s: assignments required", s.ID)
				}
			}
			if s.Kind == "if" {
				if s.Condition == nil {
					return fmt.Errorf("step %s: condition required", s.ID)
				}
				if err := walk(s.Then); err != nil {
					return err
				}
				if err := walk(s.Else); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(d.Steps)
}
