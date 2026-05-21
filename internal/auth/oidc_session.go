package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const oidcCookieName = "pca_oidc"

type oidcFlowData struct {
	State        string `json:"state"`
	Nonce        string `json:"nonce"`
	CodeVerifier string `json:"code_verifier"`
	TenantSlug   string `json:"tenant_slug"`
	ExpiresUnix  int64  `json:"exp"`
}

func signOIDCData(secret string, data oidcFlowData) (string, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func verifyOIDCData(secret, cookie string) (*oidcFlowData, error) {
	parts := strings.Split(cookie, ".")
	if len(parts) != 2 {
		return nil, errors.New("oidc cookie: bad format")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return nil, errors.New("oidc cookie: bad signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var data oidcFlowData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if time.Now().Unix() > data.ExpiresUnix {
		return nil, errors.New("oidc cookie: expired")
	}
	return &data, nil
}

func newOIDCFlow(tenantSlug, state, nonce, verifier string, ttl time.Duration) (oidcFlowData, error) {
	if state == "" || nonce == "" || verifier == "" {
		return oidcFlowData{}, fmt.Errorf("oidc flow: missing state/nonce/verifier")
	}
	return oidcFlowData{
		State: state, Nonce: nonce, CodeVerifier: verifier,
		TenantSlug: tenantSlug, ExpiresUnix: time.Now().Add(ttl).Unix(),
	}, nil
}
