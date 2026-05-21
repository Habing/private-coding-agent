package agent

import (
	"context"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// MemoryComposer wraps another ContextComposer and appends a memory injection
// system message after skills (slice 16).
type MemoryComposer struct {
	inner ContextComposer
}

func WrapMemoryComposer(inner ContextComposer) *MemoryComposer {
	if inner == nil {
		inner = NoopComposer{}
	}
	return &MemoryComposer{inner: inner}
}

func (c *MemoryComposer) ComposeSystem(ctx context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error) {
	msgs, meta, err := c.inner.ComposeSystem(ctx, in)
	if err != nil {
		return nil, ComposeMeta{}, err
	}
	if in.MemorySection != "" {
		msgs = append(msgs, modelgw.ChatMessage{
			Role:    modelgw.RoleSystem,
			Content: in.MemorySection,
		})
		meta.MemoryIDs = in.MemoryIDs
		meta.MemoryCharCount = in.MemoryCharCount
		meta.MemoryTruncated = in.MemoryTruncated
	}
	return msgs, meta, nil
}
