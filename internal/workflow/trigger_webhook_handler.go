package workflow

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// WebhookHandler serves POST /hooks/workflow/:token (no JWT).
type WebhookHandler struct {
	svc         *Service
	ratePerMin  int
	idempotency *webhookIdempotency
	limiter     *tokenRateLimiter
}

// NewWebhookHandler builds the public webhook ingress handler.
func NewWebhookHandler(svc *Service, ratePerMin int) *WebhookHandler {
	if ratePerMin <= 0 {
		ratePerMin = 60
	}
	return &WebhookHandler{
		svc:         svc,
		ratePerMin:  ratePerMin,
		idempotency: newWebhookIdempotency(5 * time.Minute),
		limiter:     newTokenRateLimiter(ratePerMin),
	}
}

func (h *WebhookHandler) Register(r *gin.Engine) {
	r.POST("/hooks/workflow/:token", h.handle)
}

func (h *WebhookHandler) handle(c *gin.Context) {
	token := c.Param("token")
	if token == "" {
		c.Status(http.StatusNotFound)
		return
	}
	if !h.allowToken(token) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
		return
	}
	ctx := c.Request.Context()
	wctx, err := h.svc.triggers.GetWebhookTrigger(ctx, token)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	if !wctx.Trigger.Enabled || !wctx.Published {
		c.JSON(http.StatusConflict, gin.H{"error": "workflow not published"})
		return
	}
	idemKey := c.GetHeader("Idempotency-Key")
	if idemKey != "" {
		if cached, ok := h.idempotency.get(token, idemKey); ok {
			c.JSON(http.StatusCreated, cached)
			return
		}
	}
	var body map[string]any
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}
	}
	defaults, err := parseDefaultInputs(wctx.Trigger.DefaultInputs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid trigger inputs"})
		return
	}
	inputs := mergeTriggerInputs(defaults, body)
	userID, err := h.svc.resolveTriggerActor(ctx, wctx.Trigger.TenantID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no admin user for tenant"})
		return
	}
	res, err := h.svc.Invoke(ctx, wctx.Trigger.TenantID, userID, wctx.Slug, inputs, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := gin.H{
		"run_id":  res.RunID.String(),
		"status":  res.Status,
		"outputs": res.Outputs,
	}
	if res.Error != "" {
		resp["error"] = res.Error
	}
	h.svc.auditTriggerFire(wctx.Trigger.TenantID, userID, wctx.Slug, "workflow.trigger.webhook",
		map[string]any{
			"trigger_id": wctx.Trigger.TriggerID,
			"run_id":     res.RunID.String(),
			"body_bytes": c.Request.ContentLength,
		})
	if idemKey != "" {
		h.idempotency.put(token, idemKey, resp)
	}
	c.JSON(http.StatusCreated, resp)
}

type webhookIdempotency struct {
	ttl time.Duration
	mu  sync.Mutex
	m   map[string]idemEntry
}

type idemEntry struct {
	resp    gin.H
	expires time.Time
}

func newWebhookIdempotency(ttl time.Duration) *webhookIdempotency {
	return &webhookIdempotency{ttl: ttl, m: map[string]idemEntry{}}
}

func (c *webhookIdempotency) key(token, idem string) string {
	return token + "\x00" + idem
}

func (c *webhookIdempotency) get(token, idem string) (gin.H, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[c.key(token, idem)]
	if !ok || time.Now().After(e.expires) {
		delete(c.m, c.key(token, idem))
		return nil, false
	}
	return e.resp, true
}

func (c *webhookIdempotency) put(token, idem string, resp gin.H) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[c.key(token, idem)] = idemEntry{resp: resp, expires: time.Now().Add(c.ttl)}
}

type tokenRateLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]rateWindow
}

type rateWindow struct {
	count int
	reset time.Time
}

func newTokenRateLimiter(limit int) *tokenRateLimiter {
	return &tokenRateLimiter{limit: limit, windows: map[string]rateWindow{}}
}

func (h *WebhookHandler) allowToken(token string) bool {
	if h.ratePerMin <= 0 || h.limiter == nil {
		return true
	}
	return h.limiter.allow(token)
}

func (rl *tokenRateLimiter) allow(token string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	w, ok := rl.windows[token]
	if !ok || now.After(w.reset) {
		rl.windows[token] = rateWindow{count: 1, reset: now.Add(time.Minute)}
		return true
	}
	if w.count >= rl.limit {
		return false
	}
	w.count++
	rl.windows[token] = w
	return true
}
