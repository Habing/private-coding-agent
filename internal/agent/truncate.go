package agent

import (
	"encoding/json"
)

// DefaultMaxToolOutputBytes caps tool outputs fed back into the LLM context.
// Per spec §5.2: tool results > 50 KB are truncated with a summary preview.
const DefaultMaxToolOutputBytes = 50 * 1024

// TruncateToolOutput returns the original raw if it fits within max bytes;
// otherwise it wraps a preview in a small JSON envelope and reports truncated=true.
// The envelope shape is stable so downstream consumers (and the LLM) can parse it:
//
//	{"truncated":true,"original_size":<int>,"preview":"<string>"}
func TruncateToolOutput(raw json.RawMessage, max int) (json.RawMessage, bool) {
	if max <= 0 || len(raw) <= max {
		return raw, false
	}
	// Reserve ~120 bytes for the envelope overhead.
	previewLen := max - 120
	if previewLen < 16 {
		previewLen = 16
	}
	if previewLen > len(raw) {
		previewLen = len(raw)
	}
	envelope := struct {
		Truncated    bool   `json:"truncated"`
		OriginalSize int    `json:"original_size"`
		Preview      string `json:"preview"`
	}{
		Truncated:    true,
		OriginalSize: len(raw),
		Preview:      string(raw[:previewLen]),
	}
	out, _ := json.Marshal(envelope)
	return out, true
}
