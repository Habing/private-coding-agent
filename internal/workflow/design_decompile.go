package workflow

import (
	"fmt"
	"strconv"
	"strings"
)

// DecompileDesign parses DSL YAML into a WorkflowDesign when supported.
func DecompileDesign(dslYAML string) (*DesignDecompileResult, error) {
	doc, err := Parse(dslYAML)
	if err != nil {
		return nil, fmt.Errorf("design: %w", err)
	}
	d, warnings := docToDesign(doc)
	return &DesignDecompileResult{Design: d, Warnings: warnings}, nil
}

func docToDesign(doc *WorkflowDoc) (*WorkflowDesign, []string) {
	var warnings []string
	d := &WorkflowDesign{
		ID:          doc.ID,
		Name:        doc.Name,
		Description: doc.Description,
	}
	for name, spec := range doc.Inputs {
		widget := "text"
		var options []string
		if name == "scenario" {
			widget = "select"
			options = []string{"ok", "degraded"}
		}
		d.Inputs = append(d.Inputs, InputField{
			Name:    name,
			Type:    spec.Type,
			Default: spec.Default,
			Widget:  widget,
			Options: options,
		})
	}
	steps, w := decompileSteps(doc.Steps)
	warnings = append(warnings, w...)
	d.Steps = steps
	for name, expr := range doc.Outputs {
		d.Outputs = append(d.Outputs, OutputField{Name: name, Expr: expr})
	}
	return d, warnings
}

func decompileSteps(steps []Step) ([]DesignStep, []string) {
	var warnings []string
	out := make([]DesignStep, 0, len(steps))
	for _, s := range steps {
		ds, w, err := decompileStep(s)
		warnings = append(warnings, w...)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		out = append(out, ds)
	}
	return out, warnings
}

func decompileStep(s Step) (DesignStep, []string, error) {
	var warnings []string
	switch s.Kind {
	case NodeTool:
		args := make([]ArgField, 0, len(s.Args))
		for k, v := range s.Args {
			args = append(args, decompileArg(k, v))
		}
		return DesignStep{
			ID:   s.ID,
			Kind: "tool",
			Tool: s.Use,
			Args: args,
		}, warnings, nil
	case NodeAssign:
		assigns := make([]AssignField, 0, len(s.Assign))
		for k, v := range s.Assign {
			assigns = append(assigns, AssignField{
				Var:  k,
				Expr: v,
			})
		}
		return DesignStep{
			ID:          s.ID,
			Kind:        "assign",
			Assignments: assigns,
		}, warnings, nil
	case NodeIf:
		cond, err := parseIfCondition(s.If)
		if err != nil {
			return DesignStep{}, warnings, fmt.Errorf("step %s: %w", s.ID, err)
		}
		then, w1 := decompileSteps(s.Then)
		elseSteps, w2 := decompileSteps(s.Else)
		warnings = append(warnings, w1...)
		warnings = append(warnings, w2...)
		return DesignStep{
			ID:        s.ID,
			Kind:      "if",
			Condition: cond,
			Then:      then,
			Else:      elseSteps,
		}, warnings, nil
	case NodeForeach, NodeParallel, NodeWait:
		return DesignStep{}, warnings, fmt.Errorf("step %s: kind %s not supported in design editor", s.ID, s.Kind)
	default:
		return DesignStep{}, warnings, fmt.Errorf("step %s: unknown kind", s.ID)
	}
}

func decompileArg(name string, v any) ArgField {
	switch x := v.(type) {
	case string:
		kind := "literal"
		if isExprToken(x) || strings.Contains(x, "${") {
			kind = "expr"
		}
		return ArgField{Name: name, Value: x, ValueKind: kind}
	default:
		return ArgField{Name: name, Value: fmt.Sprint(x), ValueKind: "literal"}
	}
}

func parseIfCondition(ifExpr string) (*DesignCondition, error) {
	inner := stripExprWrapper(strings.TrimSpace(ifExpr))
	for _, spec := range []struct {
		op  string
		key string
	}{
		{"==", "eq"},
		{"!=", "ne"},
		{"<=", "le"},
		{">=", "ge"},
		{"<", "lt"},
		{">", "gt"},
	} {
		if i := strings.Index(inner, spec.op); i >= 0 {
			left := strings.TrimSpace(inner[:i])
			right := strings.TrimSpace(inner[i+len(spec.op):])
			rk := "literal"
			rv := right
			if strings.HasPrefix(right, `"`) && strings.HasSuffix(right, `"`) {
				rv, _ = strconv.Unquote(right)
			} else if len(right) >= 2 && right[0] == '\'' && right[len(right)-1] == '\'' {
				rv = strings.Trim(right, "'")
			} else if isPathLike(right) {
				rk = "expr"
				if !strings.HasPrefix(rv, "${") {
					rv = "${" + rv + "}"
				}
			}
			lk := "expr"
			lv := left
			if strings.HasPrefix(left, `"`) && strings.HasSuffix(left, `"`) {
				if u, err := strconv.Unquote(left); err == nil {
					lv = u
					lk = "literal"
				}
			} else if isPathLike(left) {
				if !strings.HasPrefix(lv, "${") {
					lv = "${" + lv + "}"
				}
			} else {
				lv = strings.Trim(left, `"'`)
				lk = "literal"
			}
			return &DesignCondition{Left: lv, LeftKind: lk, Op: spec.key, Right: rv, RightKind: rk}, nil
		}
	}
	return nil, fmt.Errorf("unsupported if expression %q (use binary comparison)", ifExpr)
}

func isPathLike(s string) bool {
	return strings.HasPrefix(s, "inputs.") ||
		strings.HasPrefix(s, "vars.") ||
		strings.HasPrefix(s, "steps.")
}
