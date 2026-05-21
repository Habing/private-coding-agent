package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/logx"
)

func newTestEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/echo", func(c *gin.Context) {
		id := logx.RequestIDFromCtx(c.Request.Context())
		c.String(http.StatusOK, id)
	})
	return r
}

func TestRequestIDMiddleware_GeneratesWhenAbsent(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	id := w.Header().Get(HeaderRequestID)
	require.NotEmpty(t, id)
	_, err := uuid.Parse(id)
	require.NoError(t, err, "generated id must be a UUID")
	require.Equal(t, id, w.Body.String(), "ctx id must match response header")
}

func TestRequestIDMiddleware_PassesThroughClientHeader(t *testing.T) {
	r := newTestEngine()
	req := httptest.NewRequest(http.MethodGet, "/echo", nil)
	req.Header.Set(HeaderRequestID, "client-supplied-id")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, "client-supplied-id", w.Header().Get(HeaderRequestID))
	require.Equal(t, "client-supplied-id", w.Body.String())
}
