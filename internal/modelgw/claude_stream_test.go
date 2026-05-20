package modelgw

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const claudeTextOnlyStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

func TestConvertClaudeStream_TextOnly(t *testing.T) {
	var got []ChatStreamChunk
	err := ConvertClaudeStream(strings.NewReader(claudeTextOnlyStream), "claude", "x",
		func(c ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)

	// 期望: role chunk + 2 个 text deltas + final usage chunk
	require.Len(t, got, 4)
	require.Equal(t, RoleAssistant, got[0].Choices[0].Delta.Role)
	require.Equal(t, "hello", got[1].Choices[0].Delta.Content)
	require.Equal(t, " world", got[2].Choices[0].Delta.Content)
	require.NotNil(t, got[3].Usage)
	require.Equal(t, 10, got[3].Usage.PromptTokens)
	require.Equal(t, 5, got[3].Usage.CompletionTokens)
	require.NotNil(t, got[3].Choices[0].FinishReason)
	require.Equal(t, "stop", *got[3].Choices[0].FinishReason)
}

const claudeToolUseStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_2","usage":{"input_tokens":20,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"calling"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"ls","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}
`

func TestConvertClaudeStream_ToolUse(t *testing.T) {
	var got []ChatStreamChunk
	err := ConvertClaudeStream(strings.NewReader(claudeToolUseStream), "claude", "x",
		func(c ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)

	// 期望: role + "calling" + tool_call chunk + final usage chunk
	require.Len(t, got, 4)
	require.Equal(t, RoleAssistant, got[0].Choices[0].Delta.Role)
	require.Equal(t, "calling", got[1].Choices[0].Delta.Content)

	require.Len(t, got[2].Choices[0].Delta.ToolCalls, 1)
	tc := got[2].Choices[0].Delta.ToolCalls[0]
	require.Equal(t, "tu_1", tc.ID)
	require.Equal(t, "ls", tc.Function.Name)
	require.JSONEq(t, `{"path":"/"}`, tc.Function.Arguments)

	require.NotNil(t, got[3].Usage)
	require.Equal(t, 20, got[3].Usage.PromptTokens)
	require.Equal(t, 12, got[3].Usage.CompletionTokens)
	require.NotNil(t, got[3].Choices[0].FinishReason)
	require.Equal(t, "tool_calls", *got[3].Choices[0].FinishReason)
}

func TestConvertClaudeStream_YieldErrorPropagates(t *testing.T) {
	myErr := errInjected
	err := ConvertClaudeStream(strings.NewReader(claudeTextOnlyStream), "claude", "x",
		func(c ChatStreamChunk) error { return myErr })
	require.ErrorIs(t, err, myErr)
}

var errInjected = &injErr{}

type injErr struct{}

func (e *injErr) Error() string { return "injected" }
