package workflow

import (
	"fmt"
	"regexp"
	"time"

	"github.com/robfig/cron/v3"
)

// triggerIDPattern matches DSL triggers[].id (lowercase slug style).
var triggerIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func validateTriggers(triggers []TriggerSpec, stepIDs map[string]bool) error {
	if len(triggers) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for i := range triggers {
		tr := &triggers[i]
		label := tr.ID
		if label == "" {
			label = fmt.Sprintf("[%d]", i)
		}
		if !triggerIDPattern.MatchString(tr.ID) {
			return fmt.Errorf("trigger %s: id must match %s", label, triggerIDPattern.String())
		}
		if seen[tr.ID] {
			return fmt.Errorf("duplicate trigger id %q", tr.ID)
		}
		seen[tr.ID] = true
		if stepIDs[tr.ID] {
			return fmt.Errorf("trigger id %q conflicts with step id", tr.ID)
		}
		hasCron := tr.Cron != ""
		hasWebhook := tr.Webhook != nil
		if hasCron == hasWebhook {
			return fmt.Errorf("trigger %s: must populate exactly one of cron or webhook", tr.ID)
		}
		if hasCron {
			if _, err := cron.ParseStandard(tr.Cron); err != nil {
				return fmt.Errorf("trigger %s: invalid cron %q: %w", tr.ID, tr.Cron, err)
			}
			tz := tr.Timezone
			if tz == "" {
				tz = "UTC"
				tr.Timezone = tz
			}
			if _, err := time.LoadLocation(tz); err != nil {
				return fmt.Errorf("trigger %s: invalid timezone %q: %w", tr.ID, tz, err)
			}
		}
		if hasWebhook {
			if err := validateWebhookSpec(tr.ID, tr.Webhook); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateWebhookSpec(triggerID string, spec map[string]any) error {
	if len(spec) == 0 {
		return nil
	}
	if len(spec) == 1 {
		if enabled, ok := spec["enabled"]; ok {
			b, ok := enabled.(bool)
			if !ok {
				return fmt.Errorf("trigger %s: webhook.enabled must be boolean", triggerID)
			}
			if !b {
				return fmt.Errorf("trigger %s: webhook.enabled must be true when set", triggerID)
			}
			return nil
		}
	}
	return fmt.Errorf("trigger %s: webhook must be {} or {enabled: true}", triggerID)
}
