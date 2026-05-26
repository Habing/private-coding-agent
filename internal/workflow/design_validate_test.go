package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateDesign_requiresSteps(t *testing.T) {
	d := &WorkflowDesign{ID: "w-1", Name: "W1", Steps: []DesignStep{}}
	err := ValidateDesign(d, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least one step")
}

func TestValidateDesign_assignRequiresBindings(t *testing.T) {
	d := &WorkflowDesign{
		ID:   "w-1",
		Name: "W1",
		Steps: []DesignStep{{
			ID: "a", Kind: "assign", Assignments: nil,
		}},
	}
	err := ValidateDesign(d, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "assignments required")
}
