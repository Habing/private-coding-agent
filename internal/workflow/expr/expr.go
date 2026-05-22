// Package expr is the v1 workflow expression evaluator. It implements two
// surfaces: Resolve (template strings with ${...} substitutions, preserving
// types when the template is a single ${expr}) and EvalBool (truthiness +
// equality + comparison + logical combinators with short-circuit).
//
// The grammar is deliberately tiny — no arithmetic, no function calls, no
// nested parentheses. The package exists to avoid a new module dependency for
// v1; Slice 20+ can swap in expr-lang/expr without changing DSL surface.
package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// StepResult is the per-step record exposed to expressions through the path
// ${steps.<id>.output[.path...]} or ${steps.<id>.error}.
type StepResult struct {
	Output any
	Error  string
}

// Scope is the read-only view passed into the evaluator.
type Scope struct {
	Inputs map[string]any
	Vars   map[string]any
	Steps  map[string]StepResult
}

// Resolve evaluates a template string. If the entire input is a single
// ${expr}, it returns the resolved value with its native Go type. Otherwise it
// returns the fmt.Sprint-concatenated string.
func Resolve(template string, sc Scope) (any, error) {
	t := strings.TrimSpace(template)
	if strings.HasPrefix(t, "${") && strings.HasSuffix(t, "}") && countTopLevel(t) == 1 {
		return EvalExpr(t[2:len(t)-1], sc)
	}
	var b strings.Builder
	i := 0
	for i < len(template) {
		if i+1 < len(template) && template[i] == '$' && template[i+1] == '{' {
			end := strings.Index(template[i+2:], "}")
			if end < 0 {
				return nil, fmt.Errorf("unterminated ${ at offset %d", i)
			}
			inner := template[i+2 : i+2+end]
			v, err := EvalExpr(inner, sc)
			if err != nil {
				return nil, err
			}
			b.WriteString(fmt.Sprint(v))
			i = i + 2 + end + 1
			continue
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String(), nil
}

// countTopLevel returns the number of top-level ${...} segments in s. Used to
// disambiguate "is this template exactly one ${expr}?" from "the literal text
// happened to start with ${ and end with }".
func countTopLevel(s string) int {
	n := 0
	depth := 0
	for i := 0; i < len(s); i++ {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			if depth == 0 {
				n++
			}
			depth++
			i++
		} else if s[i] == '}' && depth > 0 {
			depth--
		}
	}
	return n
}

// EvalExpr resolves an expression (no surrounding ${}) to a native Go value.
// For a bare path it returns the value at that path; for comparisons and
// logical operators it returns bool.
func EvalExpr(expr string, sc Scope) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}
	if b, ok := tryBool(expr, sc); ok {
		return b, nil
	}
	// Bare path / literal
	if v, err := lookup(expr, sc); err == nil {
		return v, nil
	} else {
		// Maybe it's a literal? (string in quotes / number / bool / null)
		if lit, ok2 := tryLiteral(expr); ok2 {
			return lit, nil
		}
		return nil, err
	}
}

// EvalBool evaluates an expression and coerces the result to a bool using the
// truthiness rules documented at the package level.
func EvalBool(expr string, sc Scope) (bool, error) {
	v, err := EvalExpr(expr, sc)
	if err != nil {
		return false, err
	}
	return truthy(v), nil
}

// tryBool attempts to parse expr as a boolean composition: && / || (left-to-
// right, equal precedence) and unary !. If none of the boolean operators are
// at the top level it returns (_, false) so the caller falls through to
// literal/path lookup.
func tryBool(expr string, sc Scope) (bool, bool) {
	if has, lhs, rhs := splitTopLevel(expr, "||"); has {
		l, err := EvalBool(lhs, sc)
		if err != nil {
			return false, true // surface as false; caller already saw the error path
		}
		if l {
			return true, true
		}
		r, err := EvalBool(rhs, sc)
		return r && err == nil, true
	}
	if has, lhs, rhs := splitTopLevel(expr, "&&"); has {
		l, err := EvalBool(lhs, sc)
		if err != nil || !l {
			return false, true
		}
		r, err := EvalBool(rhs, sc)
		return r && err == nil, true
	}
	// Comparisons (process before unary ! so `!x == y` ≠ `!(x == y)`; we use
	// `!x` as unary on a path only).
	for _, op := range []string{"==", "!=", "<=", ">=", "<", ">"} {
		if has, lhs, rhs := splitTopLevel(expr, op); has {
			l, err1 := resolveOperand(lhs, sc)
			r, err2 := resolveOperand(rhs, sc)
			if err1 != nil || err2 != nil {
				return false, true
			}
			return cmp(l, r, op), true
		}
	}
	if strings.HasPrefix(strings.TrimSpace(expr), "!") {
		inner := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(expr), "!"))
		v, err := EvalExpr(inner, sc)
		if err != nil {
			return false, true
		}
		return !truthy(v), true
	}
	return false, false
}

// splitTopLevel finds the first occurrence of op outside any ${}; returns the
// trimmed left/right slices when found.
func splitTopLevel(s, op string) (bool, string, string) {
	depth := 0
	for i := 0; i+len(op) <= len(s); i++ {
		if i+1 < len(s) && s[i] == '$' && s[i+1] == '{' {
			depth++
			i++
			continue
		}
		if depth > 0 && s[i] == '}' {
			depth--
			continue
		}
		if depth == 0 && s[i:i+len(op)] == op {
			// Avoid mis-splitting "==" when scanning for "=", etc.
			if op == "<" || op == ">" {
				if i+1 < len(s) && s[i+1] == '=' {
					continue
				}
			}
			if op == "=" || op == "!" {
				continue
			}
			return true, strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(op):])
		}
	}
	return false, "", ""
}

func resolveOperand(s string, sc Scope) (any, error) {
	s = strings.TrimSpace(s)
	if lit, ok := tryLiteral(s); ok {
		return lit, nil
	}
	return lookup(s, sc)
}

func tryLiteral(s string) (any, bool) {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1], true
		}
	}
	switch s {
	case "true":
		return true, true
	case "false":
		return false, true
	case "null", "nil":
		return nil, true
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, true
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return nil, false
}

// lookup resolves a dot-path: inputs.x / vars.y / steps.id.output[.path] /
// steps.id.error. Numeric path components index into slices.
func lookup(path string, sc Scope) (any, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("empty path")
	}
	parts := strings.Split(path, ".")
	var root any
	switch parts[0] {
	case "inputs":
		root = sc.Inputs
	case "vars":
		root = sc.Vars
	case "steps":
		if len(parts) < 2 {
			return nil, fmt.Errorf("steps requires step id")
		}
		sr, ok := sc.Steps[parts[1]]
		if !ok {
			return nil, fmt.Errorf("unknown step %q", parts[1])
		}
		if len(parts) == 2 {
			return sr, nil
		}
		switch parts[2] {
		case "output":
			root = sr.Output
			parts = parts[3:]
		case "error":
			if len(parts) > 3 {
				return nil, fmt.Errorf("steps.%s.error has no children", parts[1])
			}
			return sr.Error, nil
		default:
			return nil, fmt.Errorf("steps.%s.%s: expected output|error", parts[1], parts[2])
		}
		return walk(root, parts)
	default:
		return nil, fmt.Errorf("unknown root %q (want inputs|vars|steps)", parts[0])
	}
	return walk(root, parts[1:])
}

// walk descends through path segments. Each segment may be a map key or a
// numeric slice index.
func walk(root any, segs []string) (any, error) {
	cur := root
	for _, seg := range segs {
		if cur == nil {
			return nil, nil
		}
		switch v := cur.(type) {
		case map[string]any:
			cur = v[seg]
		case map[any]any:
			cur = v[seg]
		case []any:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("array index %q invalid", seg)
			}
			cur = v[idx]
		default:
			return nil, fmt.Errorf("cannot descend into %T at %q", cur, seg)
		}
	}
	return cur, nil
}

// Truthy is the exported truthiness rule, exposed so the engine can reuse it
// after resolving a template (instead of evaluating a raw expression).
func Truthy(v any) bool { return truthy(v) }

// truthy: nil/false/0/""/empty slice/empty map → false; otherwise true.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case map[any]any:
		return len(x) > 0
	default:
		return true
	}
}

// cmp implements equality + numeric ordering. Equality compares strings,
// bools, and numbers across compatible types (int vs float coerce both ways).
func cmp(l, r any, op string) bool {
	switch op {
	case "==":
		return eq(l, r)
	case "!=":
		return !eq(l, r)
	}
	ln, lok := toFloat(l)
	rn, rok := toFloat(r)
	if !lok || !rok {
		return false
	}
	switch op {
	case "<":
		return ln < rn
	case "<=":
		return ln <= rn
	case ">":
		return ln > rn
	case ">=":
		return ln >= rn
	}
	return false
}

func eq(l, r any) bool {
	if l == nil || r == nil {
		return l == r
	}
	ln, lok := toFloat(l)
	rn, rok := toFloat(r)
	if lok && rok {
		return ln == rn
	}
	// Same-type direct compare
	switch lv := l.(type) {
	case string:
		rv, ok := r.(string)
		return ok && lv == rv
	case bool:
		rv, ok := r.(bool)
		return ok && lv == rv
	}
	return false
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// ensure unicode used (lints sometimes complain about unused imports if path
// classification changes later); guard so the file always compiles cleanly.
var _ = unicode.IsLetter
