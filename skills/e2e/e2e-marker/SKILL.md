---
name: e2e-marker
description: E2E test marker skill — its body contains E2E_SKILL_MARKER_V1 so the mock provider can detect successful injection.
---

# E2E Marker

E2E_SKILL_MARKER_V1

This skill exists only so the integration test can confirm that the Skills
subsystem successfully injected its body into the model's system prompt. The
mock provider scans the system message for the literal string above and replies
with `skill-marker-ok` when present.

Do not load this skill in production profiles.
