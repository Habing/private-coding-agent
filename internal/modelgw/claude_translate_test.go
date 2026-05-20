package modelgw

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToAnthropic_SystemsJoined(t *testing.T) {
	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "a"},
			{Role: RoleSystem, Content: "b"},
			{Role: RoleUser, Content: "hi"},
		},
	}
	out := ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Equal(t, "a\n\nb", out.System)
	require.Len(t, out.Messages, 1)
	require.Equal(t, "user", out.Messages[0].Role)
}

func TestToAnthropic_MaxTokensDefault(t *testing.T) {
	out := ToAnthropicReq(ChatRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "x"}},
	}, "x")
	require.Equal(t, 4096, out.MaxTokens)

	mt := 100
	out = ToAnthropicReq(ChatRequest{
		MaxTokens: &mt,
		Messages:  []ChatMessage{{Role: RoleUser, Content: "x"}},
	}, "x")
	require.Equal(t, 100, out.MaxTokens)
}

func TestToAnthropic_ToolMessageBecomesUserToolResult(t *testing.T) {
	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: RoleAssistant, ToolCalls: []ToolCall{{
				ID: "call_1", Type: "function",
				Function: ToolCallFunc{Name: "ls", Arguments: `{"path":"/"}`},
			}}},
			{Role: RoleTool, ToolCallID: "call_1", Content: "file1\nfile2"},
		},
	}
	out := ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Len(t, out.Messages, 2)
	require.Equal(t, "assistant", out.Messages[0].Role)
	require.NotNil(t, out.Messages[0].Content[0].ToolUse)
	require.Equal(t, "call_1", out.Messages[0].Content[0].ToolUse.ID)
	require.Equal(t, "user", out.Messages[1].Role)
	require.NotNil(t, out.Messages[1].Content[0].ToolResult)
	require.Equal(t, "call_1", out.Messages[1].Content[0].ToolResult.ToolUseID)
	require.Equal(t, "file1\nfile2", out.Messages[1].Content[0].ToolResult.Content)
}

func TestToAnthropic_ToolDef(t *testing.T) {
	req := ChatRequest{
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
		Tools: []ToolDef{{
			Type: "function",
			Function: ToolDefFunction{
				Name: "ls", Description: "list files",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
	}
	out := ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Len(t, out.Tools, 1)
	require.Equal(t, "ls", out.Tools[0].Name)
}

func TestFromAnthropic_BasicText(t *testing.T) {
	in := unmarshalAnthropicResp(t, `{
		"id": "msg_1", "type": "message", "role": "assistant",
		"content": [{"type":"text","text":"hello"}],
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 1}
	}`)
	out := FromAnthropicResp(in, "claude", "claude-sonnet-4-5")
	require.Equal(t, "claude:claude-sonnet-4-5", out.Model)
	require.Equal(t, "hello", out.Choices[0].Message.Content)
	require.Equal(t, "stop", out.Choices[0].FinishReason)
	require.Equal(t, 5, out.Usage.PromptTokens)
	require.Equal(t, 1, out.Usage.CompletionTokens)
	require.Equal(t, 6, out.Usage.TotalTokens)
}

func TestFromAnthropic_ToolUse(t *testing.T) {
	in := unmarshalAnthropicResp(t, `{
		"id": "msg_1", "type": "message", "role": "assistant",
		"content": [
			{"type":"text","text":"calling ls "},
			{"type":"tool_use","id":"tu_1","name":"ls","input":{"path":"/"}}
		],
		"model": "claude-sonnet-4-5",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`)
	out := FromAnthropicResp(in, "claude", "claude-sonnet-4-5")
	require.Equal(t, "calling ls ", out.Choices[0].Message.Content)
	require.Equal(t, "tool_calls", out.Choices[0].FinishReason)
	require.Len(t, out.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "tu_1", out.Choices[0].Message.ToolCalls[0].ID)
	require.JSONEq(t, `{"path":"/"}`, out.Choices[0].Message.ToolCalls[0].Function.Arguments)
}

func TestStopReasonMapping(t *testing.T) {
	cases := map[string]string{
		"end_turn": "stop", "max_tokens": "length",
		"tool_use": "tool_calls", "stop_sequence": "stop",
		"unknown": "stop",
	}
	for in, want := range cases {
		require.Equal(t, want, mapAnthropicStopReason(in), "input %q", in)
	}
}

// 测试 helper: 把 raw JSON 反序列化到包内非导出类型 anthropicMessagesResp。
func unmarshalAnthropicResp(t *testing.T, s string) anthropicMessagesResp {
	t.Helper()
	var raw anthropicMessagesResp
	require.NoError(t, json.Unmarshal([]byte(s), &raw))
	return raw
}
