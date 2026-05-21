package auth

import (
	"fmt"
	"os"
)

// OIDCConfig holds IdP connection settings (slice 15).
type OIDCConfig struct {
	Enabled         bool
	Issuer          string
	ClientID        string
	ClientSecretEnv string
	RedirectURL     string
	TenantSlug      string
}

// ClientSecret reads the client secret from the named environment variable.
func (c OIDCConfig) ClientSecret() (string, error) {
	if c.ClientSecretEnv == "" {
		return "", fmt.Errorf("oidc: client_secret_env not configured")
	}
	sec := os.Getenv(c.ClientSecretEnv)
	if sec == "" {
		return "", fmt.Errorf("oidc: env %s is empty", c.ClientSecretEnv)
	}
	return sec, nil
}

func (c OIDCConfig) Valid() error {
	if !c.Enabled {
		return nil
	}
	if c.Issuer == "" || c.ClientID == "" || c.RedirectURL == "" {
		return fmt.Errorf("oidc: issuer, client_id, redirect_url required when enabled")
	}
	if _, err := c.ClientSecret(); err != nil {
		return err
	}
	if c.TenantSlug == "" {
		return fmt.Errorf("oidc: tenant_slug required when enabled")
	}
	return nil
}
