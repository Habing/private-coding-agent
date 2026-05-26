package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// CompileDesign turns a WorkflowDesign into DSL YAML (caller should Parse/Validate).
func CompileDesign(d *WorkflowDesign) (*DesignCompileResult, error) {
	if d == nil {
		return nil, fmt.Errorf("design: nil")
	}
	if strings.TrimSpace(d.ID) == "" {
		return nil, fmt.Errorf("design: id required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return nil, fmt.Errorf("design: name required")
	}
	doc, warnings, err := designToDoc(d)
	if err != nil {
		return nil, err
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("design: marshal: %w", err)
	}
	return &DesignCompileResult{DSLYAML: string(raw), Warnings: warnings}, nil
}

type compileDoc struct {
	ID          string                 `yaml:"id"`
	Name        string                 `yaml:"name"`
	Description string                 `yaml:"description,omitempty"`
	Inputs      map[string]compileInput  `yaml:"inputs,omitempty"`
	Steps       []yamlStep             `yaml:"steps"`
	Outputs     map[string]string      `yaml:"outputs,omitempty"`
}

type compileInput struct {
	Type        string `yaml:"type"`
	Default     any    `yaml:"default,omitempty"`
	Description string `yaml:"description,omitempty"`
}

type yamlStep struct {
	ID     string            `yaml:"id"`
	Use    string            `yaml:"use,omitempty"`
	Args   map[string]any    `yaml:"args,omitempty"`
	Assign map[string]string `yaml:"assign,omitempty"`
	If     string            `yaml:"if,omitempty"`
	Then   []yamlStep        `yaml:"then,omitempty"`
	Else   []yamlStep        `yaml:"else,omitempty"`
}

func designToDoc(d *WorkflowDesign) (*compileDoc, []string, error) {
	var warnings []string
	doc := &compileDoc{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Outputs:     map[string]string{},
	}
	if len(d.Inputs) > 0 {
		doc.Inputs = make(map[string]compileInput, len(d.Inputs))
		for _, in := range d.Inputs {
			if in.Name == "" {
				return nil, warnings, fmt.Errorf("design: input name required")
			}
			doc.Inputs[in.Name] = compileInput{
				Type:        defaultType(in.Type),
				Default:     in.Default,
				Description: in.Description,
			}
		}
	}
	steps, w, err := compileSteps(d.Steps)
	warnings = append(warnings, w...)
	if err != nil {
		return nil, warnings, err
	}
	doc.Steps = steps
	for _, o := range d.Outputs {
		if o.Name == "" {
			continue
		}
		doc.Outputs[o.Name] = normalizeExpr(o.Expr)
	}
	return doc, warnings, nil
}

func compileSteps(steps []DesignStep) ([]yamlStep, []string, error) {
	var warnings []string
	out := make([]yamlStep, 0, len(steps))
	seen := map[string]struct{}{}
	for _, s := range steps {
		if s.ID == "" {
			return nil, warnings, fmt.Errorf("design: step id required")
		}
		if _, ok := seen[s.ID]; ok {
			return nil, warnings, fmt.Errorf("design: duplicate step id %q", s.ID)
		}
		seen[s.ID] = struct{}{}
		cs, w, err := buildStep(s)
		warnings = append(warnings, w...)
		if err != nil {
			return nil, warnings, err
		}
		out = append(out, cs)
	}
	return out, warnings, nil
}

func buildStep(s DesignStep) (yamlStep, []string, error) {
	var warnings []string
	switch strings.ToLower(strings.TrimSpace(s.Kind)) {
	case "tool", "":
		if s.Tool == "" {
			return yamlStep{}, warnings, fmt.Errorf("step %s: tool required", s.ID)
		}
		args := make(map[string]any, len(s.Args))
		for _, a := range s.Args {
			if a.Name == "" {
				continue
			}
			args[a.Name] = compileArgValue(a)
		}
		return yamlStep{ID: s.ID, Use: s.Tool, Args: args}, warnings, nil
	case "assign":
		if len(s.Assignments) == 0 {
			return yamlStep{}, warnings, fmt.Errorf("step %s: assignments required", s.ID)
		}
		assign := make(map[string]string, len(s.Assignments))
		for _, a := range s.Assignments {
			if a.Var == "" {
				continue
			}
			assign[a.Var] = normalizeExpr(a.Expr)
		}
		return yamlStep{ID: s.ID, Assign: assign}, warnings, nil
	case "if":
		if s.Condition == nil {
			return yamlStep{}, warnings, fmt.Errorf("step %s: condition required", s.ID)
		}
		ifExpr, err := compileCondition(s.Condition)
		if err != nil {
			return yamlStep{}, warnings, err
		}
		then, w1, err := compileSteps(s.Then)
		warnings = append(warnings, w1...)
		if err != nil {
			return yamlStep{}, warnings, err
		}
		elseSteps, w2, err := compileSteps(s.Else)
		warnings = append(warnings, w2...)
		if err != nil {
			return yamlStep{}, warnings, err
		}
		return yamlStep{ID: s.ID, If: ifExpr, Then: then, Else: elseSteps}, warnings, nil
	default:
		return yamlStep{}, warnings, fmt.Errorf("step %s: unsupported kind %q", s.ID, s.Kind)
	}
}

func compileArgValue(a ArgField) any {
	v := strings.TrimSpace(a.Value)
	if strings.EqualFold(a.ValueKind, "expr") || isExprToken(v) {
		return normalizeExpr(v)
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}
	switch strings.ToLower(v) {
	case "true":
		return true
	case "false":
		return false
	}
	return v
}

func compileCondition(c *DesignCondition) (string, error) {
	op, err := conditionOpSymbol(c.Op)
	if err != nil {
		return "", err
	}
	left, err := formatConditionOperand(c.Left, c.LeftKind, true)
	if err != nil {
		return "", err
	}
	right, err := formatConditionOperand(c.Right, c.RightKind, false)
	if err != nil {
		return "", err
	}
	return "${" + left + " " + op + " " + right + "}", nil
}

func conditionOpSymbol(op string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "eq", "==":
		return "==", nil
	case "ne", "!=":
		return "!=", nil
	case "lt", "<":
		return "<", nil
	case "le", "<=":
		return "<=", nil
	case "gt", ">":
		return ">", nil
	case "ge", ">=":
		return ">=", nil
	default:
		return "", fmt.Errorf("design: unknown condition op %q", op)
	}
}

// formatConditionOperand renders one side of an if comparison for EvalExpr.
// preferPath: when kind is empty, treat bare inputs./vars./steps. as expressions.
func formatConditionOperand(value, kind string, preferPath bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`, nil
	}
	if strings.EqualFold(kind, "expr") || isExprToken(value) {
		return stripExprWrapper(value), nil
	}
	if preferPath && kind == "" && isPathLike(stripExprWrapper(value)) {
		return stripExprWrapper(value), nil
	}
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return value, nil
	}
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return value, nil
	}
	switch strings.ToLower(value) {
	case "true", "false", "null", "nil":
		return value, nil
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value, nil
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`, nil
}

func normalizeExpr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if isExprToken(s) {
		return s
	}
	return "${" + stripExprWrapper(s) + "}"
}

func isExprToken(s string) bool {
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}")
}

func stripExprWrapper(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		return strings.TrimSpace(s[2 : len(s)-1])
	}
	return s
}

func defaultType(t string) string {
	if strings.TrimSpace(t) == "" {
		return "string"
	}
	return t
}

func mustYAML(doc *compileDoc) string {
	b, _ := yaml.Marshal(doc)
	return string(b)
}
