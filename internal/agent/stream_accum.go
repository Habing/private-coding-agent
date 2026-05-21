package agent

import (
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// streamAccum merges OpenAI-compatible chat completion stream chunks into one
// assistant message.
type streamAccum struct {
	content      string
	toolCalls    []modelgw.ToolCall
	finishReason string
}

func (a *streamAccum) apply(chunk modelgw.ChatStreamChunk) (textDelta string) {
	if len(chunk.Choices) == 0 {
		return ""
	}
	ch := chunk.Choices[0]
	if ch.Delta.Content != "" {
		a.content += ch.Delta.Content
		textDelta = ch.Delta.Content
	}
	for _, dtc := range ch.Delta.ToolCalls {
		idx := dtc.Index
		for len(a.toolCalls) <= idx {
			a.toolCalls = append(a.toolCalls, modelgw.ToolCall{Type: "function"})
		}
		tc := &a.toolCalls[idx]
		if dtc.ID != "" {
			tc.ID = dtc.ID
		}
		if dtc.Type != "" {
			tc.Type = dtc.Type
		}
		if dtc.Function.Name != "" {
			tc.Function.Name = dtc.Function.Name
		}
		tc.Function.Arguments += dtc.Function.Arguments
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		a.finishReason = *ch.FinishReason
	}
	return textDelta
}

func (a *streamAccum) message() modelgw.ChatMessage {
	return modelgw.ChatMessage{
		Role:      modelgw.RoleAssistant,
		Content:   a.content,
		ToolCalls: a.toolCalls,
	}
}
