package modelgw

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/quota"
)

// Handler 暴露 /v1/chat/completions 和 /v1/embeddings。
type Handler struct {
	gw *Gateway
}

func NewHandler(gw *Gateway) *Handler { return &Handler{gw: gw} }

// Register 在 rg 上挂路由。rg 应已挂 auth.Middleware。
func (h *Handler) Register(rg *gin.RouterGroup) {
	v1 := rg.Group("/v1")
	v1.POST("/chat/completions", h.chat)
	v1.POST("/embeddings", h.embeddings)
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeAPIError(c *gin.Context, httpCode int, msg, typ, code string) {
	c.AbortWithStatusJSON(httpCode, gin.H{"error": apiError{Message: msg, Type: typ, Code: code}})
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		writeAPIError(c, http.StatusUnauthorized, "unauthorized", "auth_error", "missing_token")
		return nil, false
	}
	return cl, true
}

func (h *Handler) chat(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	if req.Stream {
		h.chatStream(c, cl, req)
		return
	}
	resp, err := h.gw.ChatCompletion(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) chatStream(c *gin.Context, cl *auth.Claims, req ChatRequest) {
	// 先在 flush headers 前做 validate / Resolve 等可能失败但不需要 SSE 的事
	// 由 Gateway.ChatCompletionStream 内部完成 validate;Resolve 错误也会在
	// 我们启动 SSE 前以普通 error 返回 (因为 yield 还没被调过)。
	// 为避免 "validate 错却已 flush headers" 的问题,显式先做一次 validate。
	if err := ValidateChatRequest(req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "validation")
		return
	}
	if _, _, err := h.gw.Registry().Resolve(cl.TenantID, req.Model); err != nil {
		mapErrorToAPI(c, err)
		return
	}

	sw, err := NewSSEWriter(c.Writer)
	if err != nil {
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
		return
	}
	streamErr := h.gw.ChatCompletionStream(c.Request.Context(), cl.TenantID, cl.UserID, req,
		func(chunk ChatStreamChunk) error { return sw.WriteChunk(chunk) })
	if streamErr != nil && !errors.Is(streamErr, c.Request.Context().Err()) {
		// 客户端断开不写错误帧 (写入也会失败)
		_ = sw.WriteError(streamErr.Error(), errorTypeFor(streamErr), errorCodeFor(streamErr))
		return
	}
	_ = sw.WriteDone()
}

func (h *Handler) embeddings(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req EmbeddingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	resp, err := h.gw.Embeddings(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func mapErrorToAPI(c *gin.Context, err error) {
	switch {
	case errors.Is(err, quota.ErrQuotaExceeded):
		writeAPIError(c, http.StatusTooManyRequests, err.Error(), "rate_limit_error", "quota_exceeded")
	case errors.Is(err, ErrModelInvalid):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "model_invalid")
	case errors.Is(err, ErrProviderNotFound):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "provider_not_found")
	case errors.Is(err, ErrUnsupportedFeature):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "unsupported_feature")
	case errors.Is(err, ErrProviderUnreachable):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "unreachable")
	case errors.Is(err, ErrProviderError):
		var pe *ProviderError
		if errors.As(err, &pe) {
			switch {
			case pe.StatusCode == http.StatusTooManyRequests:
				writeAPIError(c, http.StatusTooManyRequests, pe.Error(), "rate_limit_error", "provider_rate_limit")
				return
			case pe.StatusCode >= 500:
				writeAPIError(c, http.StatusBadGateway, pe.Error(), "provider_error", "provider_5xx")
				return
			}
			writeAPIError(c, http.StatusBadGateway, pe.Error(), "provider_error", "provider_4xx")
			return
		}
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "unknown")
	default:
		// validation 形错
		if strings.HasPrefix(err.Error(), "validation:") {
			writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "validation")
			return
		}
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
	}
}

func errorTypeFor(err error) string {
	switch {
	case errors.Is(err, quota.ErrQuotaExceeded):
		return "rate_limit_error"
	case errors.Is(err, ErrProviderUnreachable), errors.Is(err, ErrProviderError):
		return "provider_error"
	}
	return "server_error"
}

func errorCodeFor(err error) string {
	switch {
	case errors.Is(err, quota.ErrQuotaExceeded):
		return "quota_exceeded"
	case errors.Is(err, ErrProviderUnreachable):
		return "unreachable"
	case errors.Is(err, ErrProviderError):
		return "provider_error"
	}
	return "internal"
}
