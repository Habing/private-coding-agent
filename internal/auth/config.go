package auth

import (
	"errors"
	"fmt"
)

// defaultSecretPlaceholder 是配置示例里给出的占位值,不允许真正用作 secret。
const defaultSecretPlaceholder = "change-me-in-production"

// minSecretLength 是允许的最短 JWT secret 长度。HS256 使用 HMAC-SHA256,
// RFC 7518 §3.2 推荐 secret 至少与 hash 输出等长(32 字节)。
const minSecretLength = 32

// ValidateJWTConfig 在启动期对 JWT 配置做防御性校验。
//
// 它拒绝以下不安全配置:
//   - Secret 为空
//   - Secret 等于示例占位 "change-me-in-production"
//   - Secret 长度小于 minSecretLength (32)
//   - TTL <= 0 (会签出永远过期的 token)
//
// 校验失败时返回普通 error, 调用方应当中断启动。
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
