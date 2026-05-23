package template_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/workflow"
	"github.com/yourorg/private-coding-agent/internal/workflow/template"
)

func TestCatalog_ListHasFiveTemplates(t *testing.T) {
	list := template.List()
	require.Len(t, list, 5)
	ids := template.IDs()
	require.Contains(t, ids, "llm-summarize-notify")
	require.Contains(t, ids, "tool-chain")
}

func TestRender_AllTemplatesValidate(t *testing.T) {
	cases := []struct {
		id    string
		slots map[string]any
	}{
		{
			id: "cron-notify",
			slots: map[string]any{
				"schedule_cron": "0 9 * * 1-5",
				"message":       "weekly report",
				"notify_tool":   "llm.chat",
				"notify_args": map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{{"role": "user", "content": "hi"}},
				},
			},
		},
		{
			id: "webhook-forward",
			slots: map[string]any{
				"webhook_path": "github",
				"forward_tool": "llm.chat",
				"forward_args": map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{{"role": "user", "content": "fwd"}},
				},
			},
		},
		{
			id: "http-fetch-notify",
			slots: map[string]any{
				"url": "https://example.com", "method": "GET",
				"notify_tool": "llm.chat",
				"notify_args": map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{{"role": "user", "content": "notify"}},
				},
			},
		},
		{
			id: "llm-summarize-notify",
			slots: map[string]any{
				"prompt": "summarize tasks",
				"notify_tool": "llm.chat",
				"notify_args": map[string]any{
					"model": "default-mock:text",
					"messages": []map[string]string{{"role": "user", "content": "notify"}},
				},
			},
		},
		{
			id: "tool-chain",
			slots: map[string]any{
				"steps": []map[string]any{
					{"id": "a", "use": "llm.chat", "args": map[string]any{
						"model": "default-mock:text",
						"messages": []map[string]string{{"role": "user", "content": "x"}},
					}},
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			slug := "tpl-" + tc.id
			dsl, err := template.Render(tc.id, template.RenderInput{
				Slug: slug, Name: "Test " + tc.id, Description: "test", Slots: tc.slots,
			})
			require.NoError(t, err)
			doc, err := workflow.Parse(dsl)
			require.NoError(t, err)
			require.NoError(t, workflow.Validate(doc, workflow.DefaultConfig()))
			require.Equal(t, slug, doc.ID)
		})
	}
}

func TestClassifyAndExtract_Cron(t *testing.T) {
	id, slots, ok := template.ClassifyAndExtract("每周一 9 点发报告", "weekly", "Weekly")
	require.True(t, ok)
	require.Equal(t, "cron-notify", id)
	require.NotEmpty(t, slots["schedule_cron"])
	require.NotEmpty(t, slots["message"])
}

func TestValidateSlots_Required(t *testing.T) {
	def, err := template.Get("cron-notify")
	require.NoError(t, err)
	err = template.ValidateSlots(def, map[string]any{})
	require.Error(t, err)
}
