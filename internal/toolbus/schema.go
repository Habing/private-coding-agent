package toolbus

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CompileSchema compiles a JSON Schema document. The result is reusable across
// many Validate calls (thread-safe).
func CompileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("schema: parse: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("inline.json", doc); err != nil {
		return nil, fmt.Errorf("schema: add: %w", err)
	}
	s, err := c.Compile("inline.json")
	if err != nil {
		return nil, fmt.Errorf("schema: compile: %w", err)
	}
	return s, nil
}

// Validate checks input against the compiled schema. Returns an error wrapping
// ErrInvalidArguments on failure.
func Validate(s *jsonschema.Schema, input json.RawMessage) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(input))
	if err != nil {
		return fmt.Errorf("%w: input not JSON: %v", ErrInvalidArguments, err)
	}
	if err := s.Validate(inst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	return nil
}
