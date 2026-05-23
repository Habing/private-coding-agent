package workflow

import (
	"strings"
	"testing"
)

func TestValidate_Triggers_CronHappy(t *testing.T) {
	src := `
id: cron-flow
name: Cron
triggers:
  - id: every-minute
    cron: "* * * * *"
    timezone: UTC
    inputs:
      channel: team
steps:
  - id: a
    wait: 1ms
`
	if err := Validate(mustParse(t, src), DefaultConfig()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_Triggers_WebhookHappy(t *testing.T) {
	src := `
id: hook-flow
name: Hook
triggers:
  - id: inbound
    webhook: {}
steps:
  - id: a
    wait: 1ms
`
	if err := Validate(mustParse(t, src), DefaultConfig()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidate_Triggers_DuplicateID(t *testing.T) {
	src := `
id: dup-trig
name: D
triggers:
  - id: a
    cron: "* * * * *"
  - id: a
    webhook: {}
steps:
  - id: s
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "duplicate trigger") {
		t.Fatalf("expected duplicate trigger, got %v", err)
	}
}

func TestValidate_Triggers_ConflictsWithStep(t *testing.T) {
	src := `
id: conflict
name: C
triggers:
  - id: shared
    cron: "0 9 * * *"
steps:
  - id: shared
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "conflicts with step") {
		t.Fatalf("expected step conflict, got %v", err)
	}
}

func TestValidate_Triggers_BothCronAndWebhook(t *testing.T) {
	src := `
id: both
name: B
triggers:
  - id: x
    cron: "* * * * *"
    webhook: {}
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected mutual exclusion error, got %v", err)
	}
}

func TestValidate_Triggers_NeitherCronNorWebhook(t *testing.T) {
	src := `
id: neither
name: N
triggers:
  - id: x
    inputs: {}
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected missing kind error, got %v", err)
	}
}

func TestValidate_Triggers_InvalidCron(t *testing.T) {
	src := `
id: bad-cron
name: B
triggers:
  - id: x
    cron: "not a cron"
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "invalid cron") {
		t.Fatalf("expected invalid cron, got %v", err)
	}
}

func TestValidate_Triggers_InvalidTimezone(t *testing.T) {
	src := `
id: bad-tz
name: B
triggers:
  - id: x
    cron: "* * * * *"
    timezone: Not/A/Zone
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "invalid timezone") {
		t.Fatalf("expected invalid timezone, got %v", err)
	}
}

func TestValidate_Triggers_WebhookEnabledFalse(t *testing.T) {
	src := `
id: wh-off
name: W
triggers:
  - id: x
    webhook:
      enabled: false
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "enabled must be true") {
		t.Fatalf("expected webhook enabled error, got %v", err)
	}
}

func TestValidate_Triggers_BadID(t *testing.T) {
	src := `
id: bad-id
name: B
triggers:
  - id: Bad-ID
    cron: "* * * * *"
steps:
  - id: a
    wait: 1ms
`
	err := Validate(mustParse(t, src), DefaultConfig())
	if err == nil || !strings.Contains(err.Error(), "trigger Bad-ID") {
		t.Fatalf("expected bad trigger id, got %v", err)
	}
}

func TestParse_TriggersRoundTrip(t *testing.T) {
	src := `
id: rt
name: RT
triggers:
  - id: t1
    cron: "0 0 * * *"
  - id: t2
    webhook:
      enabled: true
steps:
  - id: s
    wait: 1ms
`
	doc, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Triggers) != 2 {
		t.Fatalf("triggers len=%d", len(doc.Triggers))
	}
	if doc.Triggers[0].Cron != "0 0 * * *" {
		t.Fatalf("cron=%q", doc.Triggers[0].Cron)
	}
	if doc.Triggers[1].Webhook == nil {
		t.Fatal("webhook nil")
	}
}
