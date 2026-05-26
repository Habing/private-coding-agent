package quota_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/quota"
)

func TestHandler_GetQuota(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc, _ := newSvc(t, quota.Limits{LLMTokensPerDay: 200000})
	t0 := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	svc.SetNowForTest(func() time.Time { return t0 })

	tid, uid := uuid.New(), uuid.New()
	ctx := auth.WithClaims(httptest.NewRequest(http.MethodGet, "/quota", nil).Context(), &auth.Claims{
		TenantID: tid,
		UserID:   uid,
		Role:     "member",
	})
	require.NoError(t, svc.CheckAndIncr(ctx, quota.KindLLMTokens, tid, uid, 42_000))

	h := quota.NewHandler(svc)
	r := gin.New()
	g := r.Group("/")
	g.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.WithClaims(c.Request.Context(), &auth.Claims{
			TenantID: tid,
			UserID:   uid,
			Role:     "member",
		}))
		c.Next()
	})
	h.Register(g)

	req := httptest.NewRequest(http.MethodGet, "/quota", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		LLMTokens struct {
			Used     int    `json:"used"`
			Cap      int    `json:"cap"`
			Enabled  bool   `json:"enabled"`
			ResetsAt string `json:"resets_at"`
		} `json:"llm_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, 42_000, body.LLMTokens.Used)
	require.Equal(t, 200_000, body.LLMTokens.Cap)
	require.True(t, body.LLMTokens.Enabled)
	require.Equal(t, "2026-05-25T00:00:00Z", body.LLMTokens.ResetsAt)
}
