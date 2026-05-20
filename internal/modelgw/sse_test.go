package modelgw_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestSSE_HeadersAndChunks(t *testing.T) {
	w := httptest.NewRecorder()
	sw, err := modelgw.NewSSEWriter(w)
	require.NoError(t, err)
	require.Equal(t, "text/event-stream; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))

	require.NoError(t, sw.WriteChunk(modelgw.ChatStreamChunk{
		ID: "x", Object: "chat.completion.chunk",
		Choices: []modelgw.ChatStreamChoice{{Index: 0,
			Delta: modelgw.ChatStreamDelta{Content: "hi"}}},
	}))
	require.NoError(t, sw.WriteDone())

	body := w.Body.String()
	require.True(t, strings.Contains(body, `data: {`))
	require.True(t, strings.Contains(body, `"content":"hi"`))
	require.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
}

func TestSSE_WriteError(t *testing.T) {
	w := httptest.NewRecorder()
	sw, _ := modelgw.NewSSEWriter(w)
	require.NoError(t, sw.WriteError("boom", "provider_error", "unreachable"))
	require.Contains(t, w.Body.String(), `"code":"unreachable"`)
}
