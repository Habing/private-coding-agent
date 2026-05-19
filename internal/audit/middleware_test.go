package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

// strictSpy 模拟 PG 行为：ctx 已取消时 Append 直接报错。
type strictSpy struct {
	mu      sync.Mutex
	got     []audit.Entry
	lastCtx context.Context
}

func (s *strictSpy) Append(ctx context.Context, e audit.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	s.lastCtx = ctx
	return nil
}

func TestAuditMiddleware_WritesEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &strictSpy{}
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

func TestAuditMiddleware_SurvivesCanceledRequestCtx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &strictSpy{}
	var auditErrs []error
	var mu sync.Mutex
	onErr := func(err error) {
		mu.Lock()
		auditErrs = append(auditErrs, err)
		mu.Unlock()
	}

	r := gin.New()
	r.Use(audit.Middleware(s, onErr))
	r.GET("/x", func(c *gin.Context) {
		// 模拟客户端断开 / handler 内部 cancel 请求 ctx
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, s.got, 1, "audit should be appended despite canceled request ctx")
	require.Empty(t, auditErrs, "no audit errors expected")

	// detached ctx 应带有自己的 5s deadline
	dl, ok := s.lastCtx.Deadline()
	require.True(t, ok, "ctx should have a deadline")
	require.WithinDuration(t, time.Now().Add(5*time.Second), dl, 6*time.Second)
}
