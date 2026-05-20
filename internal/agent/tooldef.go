package agent

import (
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// BuildModelTools converts toolbus tool definitions into the OpenAI tool-calling
// shape expected by modelgw.ChatRequest.Tools. Tools not present in allowlist
// are excluded.
func BuildModelTools(busTools []toolbus.ToolDef, allowlist []string) []modelgw.ToolDef {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, n := range allowlist {
		allowed[n] = struct{}{}
	}
	out := make([]modelgw.ToolDef, 0, len(busTools))
	for _, t := range busTools {
		if _, ok := allowed[t.Name]; !ok {
			continue
		}
		out = append(out, modelgw.ToolDef{
			Type: "function",
			Function: modelgw.ToolDefFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}
