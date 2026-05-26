package quota

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Handler exposes read-only quota usage for the authenticated user.
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register mounts GET /quota on a group already wrapped with auth.Middleware.
func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/quota", h.get)
}

func (h *Handler) get(c *gin.Context) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	llm, err := h.svc.GetUsage(ctx, KindLLMTokens, cl.TenantID, cl.UserID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "quota_read_failed"})
		return
	}

	resetsAt, err := h.svc.NextWindowStartUTC(KindLLMTokens)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "quota_read_failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"llm_tokens": llmTokenUsageJSON(llm, resetsAt),
	})
}

func llmTokenUsageJSON(u Usage, resetsAt time.Time) gin.H {
	out := gin.H{
		"used":    u.Used,
		"cap":     u.Cap,
		"enabled": u.Cap > 0,
	}
	if u.Cap > 0 {
		out["resets_at"] = resetsAt.UTC().Format(time.RFC3339)
	}
	return out
}
