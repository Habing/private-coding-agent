package template

import (
	"fmt"
)

// ValidateSlots checks required slots and coarse types against a template definition.
func ValidateSlots(def Definition, slots map[string]any) error {
	if slots == nil {
		slots = map[string]any{}
	}
	for _, spec := range def.Slots {
		val, ok := slots[spec.Name]
		if !ok || val == nil {
			if spec.Default != nil {
				continue
			}
			if spec.Required {
				return fmt.Errorf("slot %q is required", spec.Name)
			}
			continue
		}
		if err := checkSlotType(spec.Name, spec.Type, val); err != nil {
			return err
		}
	}
	return nil
}

func checkSlotType(name, typ string, val any) error {
	switch typ {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("slot %q: expected string", name)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("slot %q: expected object", name)
		}
	case "array":
		switch val.(type) {
		case []any, []map[string]any:
			return nil
		default:
			return fmt.Errorf("slot %q: expected array", name)
		}
	}
	return nil
}

// MergeDefaults fills missing slots from template defaults.
func MergeDefaults(def Definition, slots map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range slots {
		out[k] = v
	}
	for _, spec := range def.Slots {
		if _, ok := out[spec.Name]; !ok && spec.Default != nil {
			out[spec.Name] = spec.Default
		}
	}
	return out
}
