package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCClient wraps provider discovery, PKCE auth URLs, and token exchange.
type OIDCClient struct {
	provider *oidc.Provider
	oauth2   oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDCClient discovers the issuer and builds an OAuth2 + ID-token verifier.
func NewOIDCClient(ctx context.Context, cfg OIDCConfig) (*OIDCClient, error) {
	secret, err := cfg.ClientSecret()
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider: %w", err)
	}
	oauthCfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: secret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return &OIDCClient{
		provider: provider,
		oauth2:   oauthCfg,
		verifier: verifier,
	}, nil
}

// AuthCodeURL returns the authorize URL and PKCE verifier to store server-side.
func (c *OIDCClient) AuthCodeURL(state, nonce string) (url string, codeVerifier string, err error) {
	verifier := oauth2.GenerateVerifier()
	url = c.oauth2.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("code_challenge", oauth2.S256ChallengeFromVerifier(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	return url, verifier, nil
}

// ExchangeCode trades an authorization code for tokens and verified claims.
func (c *OIDCClient) ExchangeCode(ctx context.Context, code, codeVerifier string) (*OIDCClaims, error) {
	tok, err := c.oauth2.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("token response missing id_token")
	}
	idTok, err := c.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	_ = idTok.Claims(&claims)
	return &OIDCClaims{
		Sub:   idTok.Subject,
		Iss:   idTok.Issuer,
		Email: claims.Email,
		Name:  claims.Name,
		Nonce: idTok.Nonce,
	}, nil
}

// OIDCClaims are the identity fields used for JIT user mapping.
type OIDCClaims struct {
	Sub   string
	Iss   string
	Email string
	Name  string
	Nonce string
}
