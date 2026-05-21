// Package auth provides JWT issuance/validation and HTTP middleware for the API.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

type Claims struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	Role      string
	JTI       string    // unique token ID for revocation lookup
	ExpiresAt time.Time // surfaced so logout can compute revocation TTL
}

type JWT struct {
	cfg JWTConfig
}

// NewJWT constructs a JWT service from cfg. Panics if cfg fails
// ValidateJWTConfig — guards against accidental insecure secrets even when
// callers forget to validate at startup. main.go is expected to call
// ValidateJWTConfig first and surface a friendly error; this panic is a
// last-resort defense.
func NewJWT(cfg JWTConfig) *JWT {
	if err := ValidateJWTConfig(cfg); err != nil {
		panic("auth.NewJWT: " + err.Error())
	}
	return &JWT{cfg: cfg}
}

type jwtClaims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Issue signs an HS256 JWT carrying the user/tenant IDs and role, with
// IssuedAt set to now, ExpiresAt to now + configured TTL, and a freshly
// generated jti so individual tokens can be revoked via logout.
func (s *JWT) Issue(userID, tenantID uuid.UUID, role string) (string, error) {
	now := time.Now()
	c := jwtClaims{
		UserID:   userID.String(),
		TenantID: tenantID.String(),
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.TTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString([]byte(s.cfg.Secret))
}

// Parse verifies the token's signature and expiry, enforces an HMAC signing
// method, and returns the decoded claims with UUIDs parsed.
func (s *JWT) Parse(token string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(token, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	jc, ok := t.Claims.(*jwtClaims)
	if !ok || !t.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	uid, err := uuid.Parse(jc.UserID)
	if err != nil {
		return nil, fmt.Errorf("uid: %w", err)
	}
	tid, err := uuid.Parse(jc.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tid: %w", err)
	}
	var exp time.Time
	if jc.ExpiresAt != nil {
		exp = jc.ExpiresAt.Time
	}
	return &Claims{
		UserID: uid, TenantID: tid, Role: jc.Role,
		JTI:       jc.ID,
		ExpiresAt: exp,
	}, nil
}
