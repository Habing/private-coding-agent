package workflow

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/workflow/template"
)

// ProposalHandler exposes NL workflow proposal REST under /agent/workflow/* and
// admin approval under /admin/workflow/proposals/*.
type ProposalHandler struct {
	svc *ProposalService
}

// NewProposalHandler wires a ProposalHandler.
func NewProposalHandler(svc *ProposalService) *ProposalHandler {
	return &ProposalHandler{svc: svc}
}

// RegisterAgent mounts authenticated (member + admin) proposal routes.
func (h *ProposalHandler) RegisterAgent(rg *gin.RouterGroup) {
	g := rg.Group("/agent/workflow")
	g.GET("/templates", h.listTemplates)
	g.POST("/proposals", h.createProposal)
	g.GET("/proposals/:id/graph", h.proposalGraph)
	g.GET("/proposals/:id", h.getProposal)
	g.POST("/proposals/:id/confirm", h.confirmProposal)
}

// RegisterAdmin mounts admin-only approval routes.
func (h *ProposalHandler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.POST("/admin/workflow/proposals/:id/approve", h.approveProposal)
	rg.POST("/admin/workflow/proposals/:id/reject", h.rejectProposal)
}

func (h *ProposalHandler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *ProposalHandler) listTemplates(c *gin.Context) {
	if _, ok := h.claims(c); !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": template.List()})
}

type createProposalReq struct {
	Slug        string         `json:"slug"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	TemplateID  string         `json:"template_id"`
	Slots       map[string]any `json:"slots"`
	DSLYAML     string         `json:"dsl_yaml"`
	UserMessage string         `json:"user_message"`
	SessionID   *uuid.UUID     `json:"session_id"`
}

func (h *ProposalHandler) createProposal(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req createProposalReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if req.Slug == "" || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "slug_and_name_required"})
		return
	}

	ctx := c.Request.Context()
	var prop *Proposal
	var err error

	switch {
	case req.TemplateID != "":
		prop, err = h.svc.CreateFromTemplate(ctx, cl.TenantID, cl.UserID,
			req.TemplateID, req.Slug, req.Name, req.Description, req.Slots, req.SessionID)
	case req.DSLYAML != "":
		prop, err = h.svc.Create(ctx, cl.TenantID, cl.UserID, CreateProposalInput{
			Slug: req.Slug, Name: req.Name, Description: req.Description,
			DSLYAML: req.DSLYAML, SessionID: req.SessionID,
		})
	case req.UserMessage != "":
		tid, slots, matched := template.ClassifyAndExtract(req.UserMessage, req.Slug, req.Name)
		if !matched {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":  "no_template_match",
				"detail": "pass template_id or dsl_yaml for custom flows",
			})
			return
		}
		desc := req.Description
		if desc == "" {
			desc = req.UserMessage
		}
		prop, err = h.svc.CreateFromTemplate(ctx, cl.TenantID, cl.UserID,
			tid, req.Slug, req.Name, desc, slots, req.SessionID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "template_id_dsl_or_user_message_required"})
		return
	}
	if err != nil {
		h.writeProposalErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, proposalResponse(prop))
}

func (h *ProposalHandler) getProposal(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	prop, err := h.svc.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		h.writeProposalErr(c, err)
		return
	}
	c.JSON(http.StatusOK, proposalResponse(prop))
}

func (h *ProposalHandler) proposalGraph(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	prop, err := h.svc.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		h.writeProposalErr(c, err)
		return
	}
	g, err := GraphFromYAML(prop.DSLYAML)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "parse", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, g)
}

func (h *ProposalHandler) confirmProposal(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	isAdmin := cl.Role == "admin"
	prop, err := h.svc.Confirm(c.Request.Context(), cl.TenantID, cl.UserID, id, isAdmin)
	if err != nil {
		h.writeProposalErr(c, err)
		return
	}
	c.JSON(http.StatusOK, proposalResponse(prop))
}

func (h *ProposalHandler) approveProposal(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	prop, err := h.svc.Approve(c.Request.Context(), cl.TenantID, cl.UserID, id)
	if err != nil {
		h.writeProposalErr(c, err)
		return
	}
	c.JSON(http.StatusOK, proposalResponse(prop))
}

func (h *ProposalHandler) rejectProposal(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_id"})
		return
	}
	if err := h.svc.Reject(c.Request.Context(), cl.TenantID, cl.UserID, id); err != nil {
		h.writeProposalErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func proposalResponse(p *Proposal) gin.H {
	return gin.H{
		"proposal": p,
		"summary":  ProposalSummary(p),
	}
}

func (h *ProposalHandler) writeProposalErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrProposalNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "detail": err.Error()})
	case errors.Is(err, ErrProposalSlugPublished):
		c.JSON(http.StatusConflict, gin.H{"error": "slug_published", "detail": err.Error()})
	case errors.Is(err, ErrProposalDryRunFailed):
		c.JSON(http.StatusBadRequest, gin.H{"error": "dry_run_failed", "detail": err.Error()})
	case errors.Is(err, ErrProposalInvalidState):
		c.JSON(http.StatusConflict, gin.H{"error": "invalid_state", "detail": err.Error()})
	case errors.Is(err, ErrSlugTaken):
		c.JSON(http.StatusConflict, gin.H{"error": "slug_taken", "detail": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "validate", "detail": err.Error()})
	}
}
