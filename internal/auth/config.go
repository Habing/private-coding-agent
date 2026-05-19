package auth

import (
	"errors"
	"fmt"
)

// defaultSecretPlaceholder 是配置示例里给出的占位值,不允许真正用作 secret。
const defaultSecretPlaceholder = "change-me-in-production"

// minSecretLength 是允许的最短 JWT secret 长度。低于此值的 secret 容易被暴力破解。
const minSecretLength = 16

// ValidateJWTConfig 在启动期对 JWT 配置做防御性校验。
//
// 它拒绝以下不安全配置:
//   - Secret 为空
//   - Secret 等于示例占位 "change-me-in-production"
//   - Secret 长度小于 minSecretLength (16)
//   - TTL <= 0 (会签出永远过期的 token)
//
// 校验失败时返回 ConfigError, 调用方应当中断启动。
func ValidateJWTConfig(cfg JWTConfig) error {
	if cfg.Secret == "" {
		return errors.New("jwt: secret is empty")
	}
	if cfg.Secret == defaultSecretPlaceholder {
		return fmt.Errorf("jwt: secret is the default placeholder %q, set a real secret via PCA_AUTH_JWT_SECRET", defaultSecretPlaceholder)
	}
	if len(cfg.Secret) < minSecretLength {
		return fmt.Errorf("jwt: secret too short (%d < %d chars)", len(cfg.Secret), minSecretLength)
	}
	if cfg.TTL <= 0 {
		return fmt.Errorf("jwt: ttl must be positive (got %s)", cfg.TTL)
	}
	return nil
}
