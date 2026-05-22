package mcp

import "errors"

// ErrServerNotFound is returned by Repo.Get / Update / Delete when no row
// matches the (tenant_id, id) tuple. Handler maps it to 404.
var ErrServerNotFound = errors.New("mcp: server not found")

// ErrSlugConflict is returned by Repo.Insert when (tenant_id, slug) already
// exists. Handler maps it to 409.
var ErrSlugConflict = errors.New("mcp: slug already in use for tenant")

// ErrSlugInvalid is returned by Repo.Insert / Update if the slug fails the
// charset/length rules (regex ^[a-z0-9][a-z0-9_-]{0,62}$).
var ErrSlugInvalid = errors.New("mcp: invalid slug")

// ErrManagerDisabled is returned by the Manager and admin handler when
// cfg.MCP.Enabled is false. The handler maps it to 503.
var ErrManagerDisabled = errors.New("mcp: manager disabled")

// ErrTenantMismatch is returned by mcpTool.Invoke when a different tenant
// tries to call a tool that belongs to another tenant. Bubbles up through
// Bus.Invoke as a normal tool error.
var ErrTenantMismatch = errors.New("mcp: cross-tenant invocation refused")
