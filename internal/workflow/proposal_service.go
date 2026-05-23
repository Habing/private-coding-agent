package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/workflow/template"
)

// ProposalService coordinates NL workflow proposals: validate DSL, persist draft
// workflow rows, dry-run, and publish after confirm/approve.
type ProposalService struct {
	proposals *ProposalRepo
	workflows *Service
	audit     audit.Sink
}

// NewProposalService wires a ProposalService.
func NewProposalService(proposals *ProposalRepo, workflows *Service, sink audit.Sink) *ProposalService {
	return &ProposalService{proposals: proposals, workflows: workflows, audit: sink}
}

// Create validates DSL, upserts an unpublished workflow draft, dry-runs it, and
// stores a proposal row with the dry-run snapshot.
func (s *ProposalService) Create(ctx context.Context, tenantID, userID uuid.UUID, in CreateProposalInput) (*Proposal, error) {
	if in.Slug == "" || in.Name == "" || in.DSLYAML == "" {
		return nil, fmt.Errorf("slug, name, and dsl_yaml are required")
	}
	if _, err := s.workflows.parseValidate(in.DSLYAML, in.Slug); err != nil {
		return nil, err
	}

	source := in.Source
	if source == "" {
		source = ProposalSourceFreeform
	}
	slotsJSON, err := json.Marshal(orEmptyMap(in.Slots))
	if err != nil {
		return nil, fmt.Errorf("marshal slots: %w", err)
	}

	if err := s.ensureDraftWorkflow(ctx, tenantID, in); err != nil {
		return nil, err
	}

	dryOK, dryOut, dryErr := s.runDryRun(ctx, tenantID, userID, in.Slug, in.DSLYAML)

	prop := Proposal{
		TenantID:         tenantID,
		SessionID:        in.SessionID,
		CreatedBy:        userID,
		Slug:             in.Slug,
		Name:             in.Name,
		Description:      in.Description,
		DSLYAML:          in.DSLYAML,
		Source:           source,
		TemplateID:       in.TemplateID,
		SlotsJSON:        slotsJSON,
		DryRunOK:         dryOK,
		DryRunOutputJSON: dryOut,
		DryRunError:      dryErr,
		Status:           ProposalDraft,
	}
	created, err := s.proposals.Insert(ctx, prop)
	if err != nil {
		return nil, err
	}
	s.auditProposal(tenantID, userID, created.ID, "workflow.proposal.create", map[string]any{
		"slug": in.Slug, "source": source, "template_id": in.TemplateID, "dry_run_ok": dryOK,
	})
	return created, nil
}

// CreateFromTemplate renders a catalog template, then runs Create.
func (s *ProposalService) CreateFromTemplate(ctx context.Context, tenantID, userID uuid.UUID,
	templateID, slug, name, description string, slots map[string]any, sessionID *uuid.UUID) (*Proposal, error) {

	dsl, err := template.Render(templateID, template.RenderInput{
		Slug: slug, Name: name, Description: description, Slots: slots,
	})
	if err != nil {
		return nil, err
	}
	return s.Create(ctx, tenantID, userID, CreateProposalInput{
		Slug:        slug,
		Name:        name,
		Description: description,
		DSLYAML:     dsl,
		Source:      ProposalSourceTemplatePrefix + templateID,
		TemplateID:  templateID,
		Slots:       slots,
		SessionID:   sessionID,
	})
}

// Get returns a tenant-scoped proposal.
func (s *ProposalService) Get(ctx context.Context, tenantID, id uuid.UUID) (*Proposal, error) {
	return s.proposals.Get(ctx, tenantID, id)
}

// Confirm records user intent to publish. Admins publish immediately; members
// enter pending_approval for an admin to approve later.
func (s *ProposalService) Confirm(ctx context.Context, tenantID, userID uuid.UUID,
	proposalID uuid.UUID, isAdmin bool) (*Proposal, error) {

	prop, err := s.proposals.Get(ctx, tenantID, proposalID)
	if err != nil {
		return nil, err
	}
	if prop.Status != ProposalDraft && prop.Status != ProposalPendingApproval {
		return nil, fmt.Errorf("%w: status=%s", ErrProposalInvalidState, prop.Status)
	}
	if !prop.DryRunOK {
		return nil, ErrProposalDryRunFailed
	}

	if isAdmin {
		if err := s.publishProposal(ctx, tenantID, userID, prop); err != nil {
			return nil, err
		}
		return s.proposals.Get(ctx, tenantID, proposalID)
	}

	if err := s.proposals.SetStatus(ctx, tenantID, proposalID, ProposalPendingApproval, &userID); err != nil {
		return nil, err
	}
	s.auditProposal(tenantID, userID, proposalID, "workflow.proposal.confirm", map[string]any{
		"slug": prop.Slug, "pending_approval": true,
	})
	return s.proposals.Get(ctx, tenantID, proposalID)
}

// Approve lets an admin publish a member-submitted proposal.
func (s *ProposalService) Approve(ctx context.Context, tenantID, adminID, proposalID uuid.UUID) (*Proposal, error) {
	prop, err := s.proposals.Get(ctx, tenantID, proposalID)
	if err != nil {
		return nil, err
	}
	if prop.Status != ProposalPendingApproval {
		return nil, fmt.Errorf("%w: status=%s", ErrProposalInvalidState, prop.Status)
	}
	if !prop.DryRunOK {
		return nil, ErrProposalDryRunFailed
	}
	if err := s.publishProposal(ctx, tenantID, adminID, prop); err != nil {
		return nil, err
	}
	return s.proposals.Get(ctx, tenantID, proposalID)
}

// Reject marks a proposal rejected (admin action on pending_approval).
func (s *ProposalService) Reject(ctx context.Context, tenantID, adminID, proposalID uuid.UUID) error {
	prop, err := s.proposals.Get(ctx, tenantID, proposalID)
	if err != nil {
		return err
	}
	if prop.Status != ProposalPendingApproval && prop.Status != ProposalDraft {
		return fmt.Errorf("%w: status=%s", ErrProposalInvalidState, prop.Status)
	}
	if err := s.proposals.SetStatus(ctx, tenantID, proposalID, ProposalRejected, &adminID); err != nil {
		return err
	}
	s.auditProposal(tenantID, adminID, proposalID, "workflow.proposal.reject", map[string]any{
		"slug": prop.Slug,
	})
	return nil
}

func (s *ProposalService) publishProposal(ctx context.Context, tenantID, actorID uuid.UUID, prop *Proposal) error {
	if err := s.ensureDraftWorkflow(ctx, tenantID, CreateProposalInput{
		Slug: prop.Slug, Name: prop.Name, Description: prop.Description, DSLYAML: prop.DSLYAML,
	}); err != nil {
		return err
	}
	if err := s.workflows.Publish(ctx, tenantID, prop.Slug); err != nil {
		return err
	}
	if err := s.proposals.SetStatus(ctx, tenantID, prop.ID, ProposalPublished, &actorID); err != nil {
		return err
	}
	s.auditProposal(tenantID, actorID, prop.ID, "workflow.proposal.confirm", map[string]any{
		"slug": prop.Slug, "published": true,
	})
	return nil
}

func (s *ProposalService) ensureDraftWorkflow(ctx context.Context, tenantID uuid.UUID, in CreateProposalInput) error {
	_, err := s.workflows.Create(ctx, tenantID, in.Slug, in.Name, in.Description, in.DSLYAML)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrSlugTaken) {
		return err
	}
	existing, gerr := s.workflows.Get(ctx, tenantID, in.Slug)
	if gerr != nil {
		return gerr
	}
	if existing.Published {
		return fmt.Errorf("%w: %s", ErrProposalSlugPublished, in.Slug)
	}
	_, err = s.workflows.Update(ctx, tenantID, in.Slug, in.Name, in.Description, in.DSLYAML)
	return err
}

func (s *ProposalService) runDryRun(ctx context.Context, tenantID, userID uuid.UUID,
	slug, dsl string) (bool, json.RawMessage, string) {

	// Default inputs from DSL input defaults where possible.
	doc, err := Parse(dsl)
	if err != nil {
		return false, nil, err.Error()
	}
	inputs := defaultInputsFromDoc(doc)
	res, err := s.workflows.Invoke(ctx, tenantID, userID, slug, inputs, true)
	if err != nil {
		return false, nil, err.Error()
	}
	ok := res.Status == StatusOK
	var outJSON json.RawMessage
	if res.Outputs != nil {
		outJSON, _ = json.Marshal(res.Outputs)
	}
	errText := res.Error
	if !ok && errText == "" {
		errText = res.Status
	}
	return ok, outJSON, errText
}

func defaultInputsFromDoc(doc *WorkflowDoc) map[string]any {
	if doc == nil || len(doc.Inputs) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(doc.Inputs))
	for k, spec := range doc.Inputs {
		if spec.Default != nil {
			out[k] = spec.Default
		}
	}
	return out
}

func (s *ProposalService) auditProposal(tenantID, userID, proposalID uuid.UUID, action string, meta map[string]any) {
	if s.audit == nil {
		return
	}
	if meta == nil {
		meta = map[string]any{}
	}
	meta["proposal_id"] = proposalID.String()
	tid := tenantID
	uid := userID
	audit.Detached(s.audit, audit.Entry{
		TenantID: &tid, UserID: &uid, Action: action, Target: proposalID.String(), Metadata: meta,
	}, nil)
}
