package workflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// TriggerAdminHandler exposes trigger inspection routes on the admin group.
type TriggerAdminHandler struct {
	svc *Service
}

func NewTriggerAdminHandler(svc *Service) *TriggerAdminHandler {
	return &TriggerAdminHandler{svc: svc}
}

func (h *TriggerAdminHandler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/admin/workflows")
	g.GET("/:slug/triggers", h.list)
	g.POST("/:slug/triggers/:triggerId/run", h.run)
	g.POST("/:slug/triggers/:triggerId/rotate-token", h.rotateToken)
}

func (h *TriggerAdminHandler) list(c *gin.Context) {
	tenantID, _, ok := h.claims(c)
	if !ok {
		return
	}
	rows, err := h.svc.ListTriggers(c.Request.Context(), tenantID, c.Param("slug"))
	if err != nil {
		h.writeErr(c, err)
		return
	}
	base := webhookBaseURL(c)
	out := make([]gin.H, 0, len(rows))
	for _, tr := range rows {
		item := gin.H{
			"trigger_id":     tr.TriggerID,
			"kind":           tr.Kind,
			"enabled":        tr.Enabled,
			"cron_expr":      tr.CronExpr,
			"timezone":       tr.Timezone,
			"next_run_at":    tr.NextRunAt,
			"last_run_at":    tr.LastRunAt,
			"last_status":    tr.LastStatus,
			"last_error":     tr.LastError,
			"default_inputs": jsonRawOrEmpty(tr.DefaultInputs),
		}
		if tr.Kind == TriggerKindWebhook && tr.WebhookToken != "" {
			item["webhook_url"] = base + tr.WebhookToken
			item["webhook_token_suffix"] = tokenSuffix(tr.WebhookToken)
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{
		"triggers":         out,
		"webhook_base_url": base,
	})
}

func (h *TriggerAdminHandler) run(c *gin.Context) {
	tenantID, userID, ok := h.claims(c)
	if !ok {
		return
	}
	res, err := h.svc.RunTriggerManual(c.Request.Context(), tenantID, userID,
		c.Param("slug"), c.Param("triggerId"))
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"run_id":  res.RunID.String(),
		"status":  res.Status,
		"outputs": res.Outputs,
		"error":   res.Error,
	})
}

func (h *TriggerAdminHandler) rotateToken(c *gin.Context) {
	tenantID, _, ok := h.claims(c)
	if !ok {
		return
	}
	token, err := h.svc.RotateWebhookToken(c.Request.Context(), tenantID,
		c.Param("slug"), c.Param("triggerId"))
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"webhook_url": webhookBaseURL(c) + token,
		"token":       token,
	})
}

func (h *TriggerAdminHandler) claims(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return uuid.Nil, uuid.Nil, false
	}
	return cl.TenantID, cl.UserID, true
}

func (h *TriggerAdminHandler) writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func webhookBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if fwd := c.GetHeader("X-Forwarded-Proto"); fwd != "" {
		scheme = strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host := c.Request.Host
	if host == "" {
		host = "localhost:8080"
	}
	return scheme + "://" + host + "/hooks/workflow/"
}

func tokenSuffix(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

func jsonRawOrEmpty(raw []byte) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}
