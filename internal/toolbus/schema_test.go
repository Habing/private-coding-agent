package toolbus_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestCompileSchema_ValidObject(t *testing.T) {
	raw := json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`)
	s, err := toolbus.CompileSchema(raw)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestCompileSchema_BadJSON(t *testing.T) {
	_, err := toolbus.CompileSchema(json.RawMessage(`not json`))
	require.Error(t, err)
}

func TestValidate_OK(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	require.NoError(t, toolbus.Validate(s, json.RawMessage(`{"x":5}`)))
}

func TestValidate_MissingRequired(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	err := toolbus.Validate(s, json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestValidate_TypeMismatch(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	err := toolbus.Validate(s, json.RawMessage(`{"x":"not-int"}`))
	require.Error(t, err)
}
