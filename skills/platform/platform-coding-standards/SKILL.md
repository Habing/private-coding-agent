---
name: platform-coding-standards
description: Platform coding standards for the private-coding-agent project (Go, tests, errors, security).
---

# Platform Coding Standards

When generating or modifying Go code in this project, follow these rules:

## Style

- Standard library first; avoid new dependencies without justification.
- Wrap errors with `fmt.Errorf("context: %w", err)` so callers can `errors.Is`.
- Package-level `var` blocks for sentinel errors named `ErrXxx`.
- One package per concern (`internal/<concern>`); avoid circular deps.

## Tests

- One `_test.go` per source file; package external (`package foo_test`) where possible.
- Use `t.TempDir()` for any filesystem state.
- Use `github.com/stretchr/testify/require` for assertions.
- DB-touching tests use dockertest with `pgvector/pgvector:pg16` image (since slice 11).

## Multi-tenancy

- Every Repo method takes `tenantID, ownerUserID uuid.UUID` and includes both in `WHERE`.
- Never trust caller-provided ids without re-checking tenant scope.

## Security

- Never log raw secrets, JWTs, or model API keys.
- Path inputs must pass `filepath.Clean` + root-prefix check before any disk read.
- Outbound model calls go through `modelgw.Gateway` so audit + metrics are stamped.

## When in doubt

Match the surrounding file. Codebase conventions trump generic Go advice.
