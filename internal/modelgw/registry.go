package modelgw

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/private-coding-agent/internal/logx"
)

// ProviderRegistry 维护活跃 provider 实例缓存。
type ProviderRegistry struct {
	repo            *ProviderRepo
	factories       map[string]ProviderFactory
	refreshInterval time.Duration

	mu     sync.RWMutex
	byName map[string]Provider
}

// NewProviderRegistry 构造。factories 由调用方注入(避免 import cycle)。
func NewProviderRegistry(repo *ProviderRepo, factories map[string]ProviderFactory,
	refresh time.Duration) *ProviderRegistry {
	return &ProviderRegistry{
		repo: repo, factories: factories,
		refreshInterval: refresh,
		byName:          map[string]Provider{},
	}
}

// Start 立刻 load 一次;失败返 error。后台 refresh 由 Run 启动。
func (r *ProviderRegistry) Start(ctx context.Context) error {
	return r.reload(ctx)
}

// Run 阻塞:每 refreshInterval 刷新一次,直到 ctx 取消。
// 调用方应 `go reg.Run(ctx)`。
func (r *ProviderRegistry) Run(ctx context.Context) {
	t := time.NewTicker(r.refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.reload(ctx); err != nil {
				logx.FromCtx(ctx).Error("provider registry refresh", "err", err.Error())
			}
		}
	}
}

func (r *ProviderRegistry) reload(ctx context.Context) error {
	configs, err := r.repo.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}

	r.mu.RLock()
	old := r.byName
	r.mu.RUnlock()

	next := make(map[string]Provider, len(configs))
	for _, cfg := range configs {
		// 复用已有实例 (避免每次 refresh 重建连接池/客户端)
		if existing, ok := old[cfg.Name]; ok && existing.ID() == cfg.ID && existing.Type() == cfg.Type {
			next[cfg.Name] = existing
			continue
		}
		factory, ok := r.factories[cfg.Type]
		if !ok {
			logx.FromCtx(ctx).Warn("provider registry: no factory for type",
				"provider_type", cfg.Type, "provider_name", cfg.Name)
			continue
		}
		p, err := factory(cfg)
		if err != nil {
			logx.FromCtx(ctx).Error("provider registry: factory failed",
				"provider_name", cfg.Name, "err", err.Error())
			continue
		}
		next[cfg.Name] = p
	}

	r.mu.Lock()
	r.byName = next
	r.mu.Unlock()
	return nil
}

// Resolve 解析 "provider:model" → (Provider, model)。
// 冒号之后的全部内容视作 model(Ollama 模型名可含冒号,如 "qwen2.5:7b")。
func (r *ProviderRegistry) Resolve(modelStr string) (Provider, string, error) {
	i := strings.IndexByte(modelStr, ':')
	if i <= 0 || i == len(modelStr)-1 {
		return nil, "", ErrModelInvalid
	}
	providerName, model := modelStr[:i], modelStr[i+1:]
	r.mu.RLock()
	p, ok := r.byName[providerName]
	r.mu.RUnlock()
	if !ok {
		return nil, "", ErrProviderNotFound
	}
	return p, model, nil
}

// SeedForTest 仅用于测试:直接把 providers 填进缓存,绕过 PG reload。
// 不要在生产代码调用。
func (r *ProviderRegistry) SeedForTest(byName map[string]Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName = byName
}
