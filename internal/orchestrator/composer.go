package orchestrator

import "github.com/yourorg/private-coding-agent/internal/modelgw"

// PrependSystemHint inserts a new system message after any pre-existing
// system messages (skills / memory injections / sandbox preamble) and before
// the first non-system message. This preserves Skill priority while still
// letting the LLM see the routing hint near the top of the prompt.
//
// hint == "" returns msgs unchanged. The function returns a new slice; it
// never mutates the input.
func PrependSystemHint(msgs []modelgw.ChatMessage, hint string) []modelgw.ChatMessage {
	if hint == "" {
		return msgs
	}
	// Find the split point: first index whose role is NOT system.
	split := len(msgs)
	for i, m := range msgs {
		if m.Role != modelgw.RoleSystem {
			split = i
			break
		}
	}
	out := make([]modelgw.ChatMessage, 0, len(msgs)+1)
	out = append(out, msgs[:split]...)
	out = append(out, modelgw.ChatMessage{Role: modelgw.RoleSystem, Content: hint})
	out = append(out, msgs[split:]...)
	return out
}
