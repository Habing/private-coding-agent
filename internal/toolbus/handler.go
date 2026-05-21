package toolbus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/quota"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

// BusInterface is implemented by *Bus; handler depends on the interface to
// keep the test seam narrow.
type BusInterface interface {
	ListTools(ctx context.Context, tenantID uuid.UUID) []ToolDef
	Invoke(ctx context.Context, tenantID, userID uuid.UUID, toolName string, input json.RawMessage) (json.RawMessage, error)
}

type Handler struct{ bus BusInterface }

func NewHandler(b BusInterface) *Handler { return &Handler{bus: b} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/tools", h.list)
	rg.POST("/tools/invoke", h.invoke)
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeAPIError(c *gin.Context, code int, msg, typ, errCode string) {
	c.AbortWithStatusJSON(code, gin.H{"error": apiError{Message: msg, Type: typ, Code: errCode}})
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		writeAPIError(c, http.StatusUnauthorized, "unauthorized", "auth_error", "missing_token")
		return nil, false
	}
	return cl, true
}

func (h *Handler) list(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	tools := h.bus.ListTools(c.Request.Context(), cl.TenantID)
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

type invokeReq struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

func (h *Handler) invoke(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req invokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	if req.Tool == "" {
		writeAPIError(c, http.StatusBadRequest, "tool field required", "invalid_request_error", "tool_required")
		return
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage(`{}`)
	}
	out, err := h.bus.Invoke(c.Request.Context(), cl.TenantID, cl.UserID, req.Tool, req.Input)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"output": json.RawMessage(out)})
}

func mapErrorToAPI(c *gin.Context, err error) {
	switch {
	case errors.Is(err, quota.ErrQuotaExceeded):
		writeAPIError(c, http.StatusTooManyRequests, err.Error(), "rate_limit_error", "quota_exceeded")
	case errors.Is(err, ErrToolNotFound):
		writeAPIError(c, http.StatusNotFound, err.Error(), "invalid_request_error", "tool_not_found")
	case errors.Is(err, ErrInvalidArguments):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_arguments")
	case errors.Is(err, ErrSandboxIDRequired):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "sandbox_id_required")
	case errors.Is(err, sandbox.ErrSandboxNotFound):
		writeAPIError(c, http.StatusNotFound, err.Error(), "invalid_request_error", "sandbox_not_found")
	case errors.Is(err, sandbox.ErrSandboxNotReady):
		writeAPIError(c, http.StatusConflict, err.Error(), "invalid_request_error", "sandbox_not_ready")
	case errors.Is(err, sandbox.ErrPathOutsideWorkspace):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "path_outside_workspace")
	case errors.Is(err, sandbox.ErrTooLarge):
		writeAPIError(c, http.StatusRequestEntityTooLarge, err.Error(), "invalid_request_error", "payload_too_large")
	case errors.Is(err, modelgw.ErrUnsupportedFeature):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "unsupported_feature")
	case errors.Is(err, modelgw.ErrProviderUnreachable):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "provider_unreachable")
	case errors.Is(err, modelgw.ErrProviderError):
		var pe *modelgw.ProviderError
		if errors.As(err, &pe) && pe.StatusCode == http.StatusTooManyRequests {
			writeAPIError(c, http.StatusTooManyRequests, err.Error(), "rate_limit_error", "provider_rate_limit")
			return
		}
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "provider_error")
	default:
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
	}
}
