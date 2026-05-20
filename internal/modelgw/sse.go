package modelgw

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter 把 ChatStreamChunk 序列化为 SSE data 帧并 flush。
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter 设置 SSE headers 并立即 flush。
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flush")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // 防 nginx 缓冲
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	return &SSEWriter{w: w, flusher: fl}, nil
}

// WriteChunk 写一条 chunk。
func (s *SSEWriter) WriteChunk(c ChatStreamChunk) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteError 在 stream 中途写错误帧。caller 之后应停止写入。
func (s *SSEWriter) WriteError(errMsg, errType, errCode string) error {
	payload := map[string]any{
		"error": map[string]string{"message": errMsg, "type": errType, "code": errCode},
	}
	b, _ := json.Marshal(payload)
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteDone 写 OpenAI 风格的 [DONE] 末尾标记。
func (s *SSEWriter) WriteDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
