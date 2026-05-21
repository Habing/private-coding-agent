package agent

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// Runner is the subset of *Engine the handler depends on.
type Runner interface {
	Run(ctx context.Context, in RunInput, yield func(Event) error) error
}

// Handler exposes POST /agent/run. The current implementation is non-streaming:
// events are buffered and returned as a JSON array. Slice 6 will replace this
// with WebSocket streaming.
type Handler struct{ engine Runner }

func NewHandler(e Runner) *Handler { return &Handler{engine: e} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.POST("/agent/run", h.run)
}

type runRequest struct {
	Model    string                `json:"model"`
	Profile  string                `json:"profile"`
	Messages []modelgw.ChatMessage `json:"messages"`
	MaxSteps int                   `json:"max_steps"`
	SkillIDs []string              `json:"skill_ids,omitempty"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeAPIError(c *gin.Context, code int, msg, typ, errCode string) {
	c.AbortWithStatusJSON(code, gin.H{"error": apiError{Message: msg, Type: typ, Code: errCode}})
}

func (h *Handler) run(c *gin.Context) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		writeAPIError(c, http.StatusUnauthorized, "unauthorized", "auth_error", "missing_token")
		return
	}
	var req runRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	if req.Model == "" {
		writeAPIError(c, http.StatusBadRequest, "model required", "invalid_request_error", "model_required")
		return
	}
	if len(req.Messages) == 0 {
		writeAPIError(c, http.StatusBadRequest, "messages required", "invalid_request_error", "messages_required")
		return
	}

	in := RunInput{
		TenantID:    cl.TenantID,
		UserID:      cl.UserID,
		Model:       req.Model,
		Messages:    req.Messages,
		ProfileName: req.Profile,
		MaxSteps:    req.MaxSteps,
		SkillIDs:    req.SkillIDs,
	}

	events := make([]Event, 0, 8)
	runErr := h.engine.Run(c.Request.Context(), in, func(ev Event) error {
		events = append(events, ev)
		return nil
	})
	if runErr != nil {
		mapErrorToAPI(c, runErr, events)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func mapErrorToAPI(c *gin.Context, err error, events []Event) {
	switch {
	case errors.Is(err, ErrEmptyMessages):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "messages_required")
	case errors.Is(err, ErrUnknownProfile):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "unknown_profile")
	case errors.Is(err, ErrMaxStepsExceeded):
		c.JSON(http.StatusOK, gin.H{"events": events, "error": apiError{
			Message: err.Error(), Type: "agent_error", Code: "max_steps_exceeded",
		}})
	case errors.Is(err, ErrLLMFailed):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "llm_failed")
	case errors.Is(err, modelgw.ErrProviderUnreachable):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "provider_unreachable")
	default:
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
	}
}
