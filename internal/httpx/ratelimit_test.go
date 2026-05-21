package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/httpx"
)

func newRLEngine(t *testing.T, cfg httpx.RateLimitConfig, cl *auth.Claims) (*gin.Engine, *miniredis.Miniredis) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(func(c *gin.Context) {
		if cl != nil {
			ctx := auth.WithClaims(c.Request.Context(), cl)
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	})
	r.Use(httpx.RateLimitMiddleware(rdb, cfg))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	return r, mr
}

func TestRateLimit_AllowsUnderCap(t *testing.T) {
	cl := &auth.Claims{TenantID: uuid.New(), UserID: uuid.New()}
	r, _ := newRLEngine(t, httpx.RateLimitConfig{PerMinute: 5}, cl)
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "request %d", i+1)
	}
}

func TestRateLimit_BlocksOverCap(t *testing.T) {
	cl := &auth.Claims{TenantID: uuid.New(), UserID: uuid.New()}
	r, _ := newRLEngine(t, httpx.RateLimitConfig{PerMinute: 3}, cl)
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		require.Equal(t, http.StatusOK, w.Code)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestRateLimit_SkipsAnonymous(t *testing.T) {
	r, _ := newRLEngine(t, httpx.RateLimitConfig{PerMinute: 1}, nil)
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		require.Equal(t, http.StatusOK, w.Code)
	}
}

func TestRateLimit_IsolatesByUser(t *testing.T) {
	clA := &auth.Claims{TenantID: uuid.New(), UserID: uuid.New()}
	clB := &auth.Claims{TenantID: clA.TenantID, UserID: uuid.New()}
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(func(c *gin.Context) {
		uname := c.GetHeader("X-Who")
		var cl *auth.Claims
		switch uname {
		case "A":
			cl = clA
		case "B":
			cl = clB
		}
		if cl != nil {
			c.Request = c.Request.WithContext(auth.WithClaims(c.Request.Context(), cl))
		}
		c.Next()
	})
	r.Use(httpx.RateLimitMiddleware(rdb, httpx.RateLimitConfig{PerMinute: 1}))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	// A uses up its quota.
	for i, code := range []int{http.StatusOK, http.StatusTooManyRequests} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		req.Header.Set("X-Who", "A")
		r.ServeHTTP(w, req)
		require.Equal(t, code, w.Code, "A request %d", i)
	}
	// B is independent and still allowed.
	wB := httptest.NewRecorder()
	reqB := httptest.NewRequest(http.MethodGet, "/ping", nil)
	reqB.Header.Set("X-Who", "B")
	r.ServeHTTP(wB, reqB)
	require.Equal(t, http.StatusOK, wB.Code)
}

func TestRateLimit_FailOpenWhenRedisDown(t *testing.T) {
	cl := &auth.Claims{TenantID: uuid.New(), UserID: uuid.New()}
	gin.SetMode(gin.TestMode)
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithClaims(c.Request.Context(), cl))
		c.Next()
	})
	r.Use(httpx.RateLimitMiddleware(rdb, httpx.RateLimitConfig{PerMinute: 1}))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil).WithContext(ctx)
	r.ServeHTTP(w, req)
	// Redis ping fails fast; middleware admits the request.
	require.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimit_DisabledWhenCapZero(t *testing.T) {
	cl := &auth.Claims{TenantID: uuid.New(), UserID: uuid.New()}
	r, mr := newRLEngine(t, httpx.RateLimitConfig{PerMinute: 0}, cl)
	for i := 0; i < 100; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
		require.Equal(t, http.StatusOK, w.Code)
	}
	// No keys touched.
	require.Empty(t, mr.Keys())
}
