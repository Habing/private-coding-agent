package modelgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Anthropic SSE 事件类型(摘自其 API 文档)。
type anthropicEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Usage        anthropicUsage  `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// claudeStreamState 跨事件累积 tool_use block 的部分 JSON。
type claudeStreamState struct {
	chunkID    string
	inputTok   int
	outputTok  int
	stopReason string
	blocks     map[int]*claudeBlockBuf
	roleSent   bool
	model      string // "claude:claude-sonnet-4-5"
}

type claudeBlockBuf struct {
	typ      string // "text" / "tool_use"
	toolID   string
	toolName string
	partial  []byte // 累积 input_json_delta
}

// ConvertClaudeStream 读 Anthropic SSE 流,逐事件调 yield 发出 OpenAI chunks。
// providerName + model 用于填充 chunk.Model。
//
// 结束:Anthropic 发 message_stop;实现发末帧含 usage,然后返 nil。
func ConvertClaudeStream(body io.Reader, providerName, model string,
	yield func(ChatStreamChunk) error) error {

	state := &claudeStreamState{
		blocks: map[int]*claudeBlockBuf{},
		model:  providerName + ":" + model,
	}
	now := func() int64 { return time.Now().Unix() }

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var currentEvent string
	var dataLines [][]byte

	flushEvent := func() error {
		if currentEvent == "" || len(dataLines) == 0 {
			currentEvent = ""
			dataLines = nil
			return nil
		}
		joined := bytes.Join(dataLines, []byte("\n"))
		currentEvent = ""
		dataLines = nil
		return handleClaudeEvent(joined, state, now, yield)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(line, []byte("event: ")) {
			currentEvent = string(line[len("event: "):])
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			dataLines = append(dataLines, line[len("data: "):])
			continue
		}
	}
	// 末尾缓冲
	if err := flushEvent(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return nil
}

func handleClaudeEvent(payload []byte, s *claudeStreamState, now func() int64,
	yield func(ChatStreamChunk) error) error {

	var ev anthropicEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil // 跳过坏帧
	}

	switch ev.Type {
	case "message_start":
		var msg struct {
			ID    string         `json:"id"`
			Usage anthropicUsage `json:"usage"`
		}
		_ = json.Unmarshal(ev.Message, &msg)
		s.chunkID = msg.ID
		s.inputTok = msg.Usage.InputTokens

	case "content_block_start":
		var cb anthropicRespBlock
		_ = json.Unmarshal(ev.ContentBlock, &cb)
		s.blocks[ev.Index] = &claudeBlockBuf{
			typ: cb.Type, toolID: cb.ID, toolName: cb.Name,
		}
		// 首次发 chunk 前确保角色已发
		if !s.roleSent {
			s.roleSent = true
			if err := yield(ChatStreamChunk{
				ID: s.chunkID, Object: "chat.completion.chunk",
				Created: now(), Model: s.model,
				Choices: []ChatStreamChoice{{
					Index: 0, Delta: ChatStreamDelta{Role: RoleAssistant},
				}},
			}); err != nil {
				return err
			}
		}

	case "content_block_delta":
		var d anthropicDelta
		_ = json.Unmarshal(ev.Delta, &d)
		bb := s.blocks[ev.Index]
		if bb == nil {
			return nil
		}
		switch d.Type {
		case "text_delta":
			return yield(ChatStreamChunk{
				ID: s.chunkID, Object: "chat.completion.chunk",
				Created: now(), Model: s.model,
				Choices: []ChatStreamChoice{{
					Index: 0, Delta: ChatStreamDelta{Content: d.Text},
				}},
			})
		case "input_json_delta":
			bb.partial = append(bb.partial, []byte(d.PartialJSON)...)
		}

	case "content_block_stop":
		bb := s.blocks[ev.Index]
		if bb == nil || bb.typ != "tool_use" {
			return nil
		}
		// tool_use 完整,一次性发 OpenAI chunk
		return yield(ChatStreamChunk{
			ID: s.chunkID, Object: "chat.completion.chunk",
			Created: now(), Model: s.model,
			Choices: []ChatStreamChoice{{
				Index: 0,
				Delta: ChatStreamDelta{ToolCalls: []ToolCall{{
					ID: bb.toolID, Type: "function",
					Function: ToolCallFunc{Name: bb.toolName, Arguments: string(bb.partial)},
				}}},
			}},
		})

	case "message_delta":
		var d anthropicDelta
		_ = json.Unmarshal(ev.Delta, &d)
		if d.StopReason != "" {
			s.stopReason = d.StopReason
		}
		if ev.Usage.OutputTokens > 0 {
			s.outputTok = ev.Usage.OutputTokens
		}

	case "message_stop":
		finish := mapAnthropicStopReason(s.stopReason)
		return yield(ChatStreamChunk{
			ID: s.chunkID, Object: "chat.completion.chunk",
			Created: now(), Model: s.model,
			Choices: []ChatStreamChoice{{
				Index: 0, FinishReason: &finish,
				Delta: ChatStreamDelta{},
			}},
			Usage: &Usage{
				PromptTokens: s.inputTok, CompletionTokens: s.outputTok,
				TotalTokens: s.inputTok + s.outputTok,
			},
		})
	}

	return nil
}
