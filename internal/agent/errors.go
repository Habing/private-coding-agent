package agent

import "errors"

// Sentinel errors. Callers use errors.Is to map to HTTP status codes.
var (
	ErrUnknownProfile      = errors.New("agent: unknown profile")
	ErrEmptyMessages       = errors.New("agent: messages required")
	ErrMaxStepsExceeded    = errors.New("agent: max steps exceeded")
	ErrLLMFailed           = errors.New("agent: llm call failed")
	ErrToolCallParseFailed = errors.New("agent: tool_call arguments not valid JSON")
)
