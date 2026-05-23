package template_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow/template"
)

func TestExampleSlots_AllCatalogIDs(t *testing.T) {
	for _, id := range template.IDs() {
		slots, err := template.ExampleSlots(id)
		require.NoError(t, err, id)
		require.NotEmpty(t, slots, id)
		def, err := template.Get(id)
		require.NoError(t, err)
		require.NoError(t, template.ValidateSlots(def, template.MergeDefaults(def, slots)))
	}
}
