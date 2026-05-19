package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

type spySink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spySink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func TestAuditMiddleware_WritesEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &spySink{}
	r := gin.New()
	r.Use(audit.Middleware(s, nil))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, s.got, 1)
	require.Equal(t, "GET", s.got[0].Method)
	require.Equal(t, http.StatusOK, s.got[0].Status)
}
