package workflow

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// StartTriggerScheduler launches a background loop that fires due cron triggers.
func StartTriggerScheduler(ctx context.Context, svc *Service, cfg TriggerSchedulerConfig) {
	if svc == nil || svc.triggers == nil {
		return
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.MaxDuePerTick <= 0 {
		cfg.MaxDuePerTick = 32
	}
	sched := &triggerScheduler{svc: svc, triggers: svc.triggers, cfg: cfg}
	go sched.loop(ctx)
}

// TriggerSchedulerConfig tunes the cron polling loop.
type TriggerSchedulerConfig struct {
	PollInterval  time.Duration
	MaxDuePerTick int
}

type triggerScheduler struct {
	svc      *Service
	triggers *TriggerRepo
	cfg      TriggerSchedulerConfig
}

func (s *triggerScheduler) loop(ctx context.Context) {
	s.tick(ctx)
	ticker := time.NewTicker(s.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *triggerScheduler) tick(ctx context.Context) {
	claims, err := s.triggers.ClaimDueCron(ctx, s.cfg.MaxDuePerTick)
	if err != nil {
		slog.Warn("workflow: claim due cron", "err", err.Error())
		return
	}
	for _, claim := range claims {
		s.fire(ctx, claim)
	}
}

func (s *triggerScheduler) fire(ctx context.Context, claim CronTriggerClaim) {
	userID, err := s.svc.resolveTriggerActor(ctx, claim.TenantID)
	if err != nil {
		slog.Warn("workflow: cron trigger skip — no admin user",
			"tenant_id", claim.TenantID, "trigger_id", claim.TriggerID, "err", err.Error())
		_ = s.triggers.RecordCronRun(ctx, claim.ID, "skipped", "no admin user")
		return
	}
	inputs, err := parseDefaultInputs(claim.DefaultInputs)
	if err != nil {
		_ = s.triggers.RecordCronRun(ctx, claim.ID, "failed", err.Error())
		return
	}
	res, err := s.svc.Invoke(ctx, claim.TenantID, userID, claim.WorkflowSlug, inputs, false)
	status := StatusOK
	errText := ""
	if err != nil {
		status = StatusFailed
		errText = err.Error()
	} else if res != nil {
		status = res.Status
		if res.Error != "" {
			errText = res.Error
		}
	}
	_ = s.triggers.RecordCronRun(ctx, claim.ID, status, errText)
	s.svc.auditTriggerFire(claim.TenantID, userID, claim.WorkflowSlug, "workflow.trigger.cron",
		map[string]any{
			"trigger_id": claim.TriggerID,
			"run_id":     runIDString(res),
			"status":     status,
		})
}

func parseDefaultInputs(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out == nil {
		return map[string]any{}, nil
	}
	return out, nil
}

func runIDString(res *InvokeResult) string {
	if res == nil {
		return ""
	}
	return res.RunID.String()
}

func mergeTriggerInputs(defaults, body map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range body {
		out[k] = v
	}
	return out
}

// TenantAdminLookup resolves a tenant's system user for automated invokes.
type TenantAdminLookup interface {
	FirstAdminID(ctx context.Context, tenantID uuid.UUID) (uuid.UUID, error)
}
