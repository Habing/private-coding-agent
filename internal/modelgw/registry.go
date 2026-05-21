package modelgw

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/logx"
)

// ProviderRegistry 维护活跃 provider 实例缓存。slice 13 起按 (tenant_id, name)
// 缓存:tenantID == nil 是全局行(平台默认);非 nil 是某租户专属覆盖。
type ProviderRegistry struct {
	repo                *ProviderRepo
	factories           map[string]ProviderFactory
	refreshInterval     time.Duration
	allowGlobalFallback bool

	mu      sync.RWMutex
	global  map[string]Provider                 // name → provider (tenant_id IS NULL)
	tenants map[uuid.UUID]map[string]Provider   // tenant_id → name → provider
}

// NewProviderRegistry 构造。factories 由调用方注入(避免 import cycle)。
// allowGlobalFallback=true 时,Resolve 找不到 tenant 行会退回 global 行。
func NewProviderRegistry(repo *ProviderRepo, factories map[string]ProviderFactory,
	refresh time.Duration, allowGlobalFallback bool) *ProviderRegistry {
	return &ProviderRegistry{
		repo: repo, factories: factories,
		refreshInterval:     refresh,
		allowGlobalFallback: allowGlobalFallback,
		global:              map[string]Provider{},
		tenants:             map[uuid.UUID]map[string]Provider{},
	}
}

// Start 立刻 load 一次;失败返 error。后台 refresh 由 Run 启动。
func (r *ProviderRegistry) Start(ctx context.Context) error {
	return r.reload(ctx)
}

// Run 阻塞:每 refreshInterval 刷新一次,直到 ctx 取消。
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
	oldGlobal := r.global
	oldTenants := r.tenants
	r.mu.RUnlock()

	nextGlobal := make(map[string]Provider)
	nextTenants := make(map[uuid.UUID]map[string]Provider)

	lookupExisting := func(tenantID *uuid.UUID, name string) (Provider, bool) {
		if tenantID == nil {
			p, ok := oldGlobal[name]
			return p, ok
		}
		if m, ok := oldTenants[*tenantID]; ok {
			p, ok := m[name]
			return p, ok
		}
		return nil, false
	}

	for _, cfg := range configs {
		// 复用已有实例 (避免每次 refresh 重建连接池/客户端)
		if existing, ok := lookupExisting(cfg.TenantID, cfg.Name); ok &&
			existing.ID() == cfg.ID && existing.Type() == cfg.Type {
			if cfg.TenantID == nil {
				nextGlobal[cfg.Name] = existing
			} else {
				m, ok := nextTenants[*cfg.TenantID]
				if !ok {
					m = map[string]Provider{}
					nextTenants[*cfg.TenantID] = m
				}
				m[cfg.Name] = existing
			}
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
		if cfg.TenantID == nil {
			nextGlobal[cfg.Name] = p
		} else {
			m, ok := nextTenants[*cfg.TenantID]
			if !ok {
				m = map[string]Provider{}
				nextTenants[*cfg.TenantID] = m
			}
			m[cfg.Name] = p
		}
	}

	r.mu.Lock()
	r.global = nextGlobal
	r.tenants = nextTenants
	r.mu.Unlock()
	return nil
}

// Resolve 解析 "provider:model" → (Provider, model)。先查 tenant 专属;
// 未命中且 allowGlobalFallback=true 时退回全局行。
// 冒号之后的全部内容视作 model(Ollama 模型名可含冒号,如 "qwen2.5:7b")。
func (r *ProviderRegistry) Resolve(tenantID uuid.UUID, modelStr string) (Provider, string, error) {
	i := strings.IndexByte(modelStr, ':')
	if i <= 0 || i == len(modelStr)-1 {
		return nil, "", ErrModelInvalid
	}
	providerName, model := modelStr[:i], modelStr[i+1:]

	r.mu.RLock()
	defer r.mu.RUnlock()

	if tenantID != uuid.Nil {
		if m, ok := r.tenants[tenantID]; ok {
			if p, ok := m[providerName]; ok {
				return p, model, nil
			}
		}
	}
	if r.allowGlobalFallback || tenantID == uuid.Nil {
		if p, ok := r.global[providerName]; ok {
			return p, model, nil
		}
	}
	return nil, "", ErrProviderNotFound
}

// SeedForTest 仅用于测试:直接把全局 providers 填进缓存,绕过 PG reload。
// 不要在生产代码调用。
func (r *ProviderRegistry) SeedForTest(byName map[string]Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.global = byName
	r.tenants = map[uuid.UUID]map[string]Provider{}
}

// SeedTenantForTest 注入某租户专属 providers(用于 fallback 测试)。
func (r *ProviderRegistry) SeedTenantForTest(tenantID uuid.UUID, byName map[string]Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tenants[tenantID] = byName
}
