package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/user"
)

type miniOIDC struct {
	server   *httptest.Server
	issuer   string
	secret   string
	key      *rsa.PrivateKey
	codeNonce map[string]string
}

func startMiniOIDC(t *testing.T) *miniOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	m := &miniOIDC{key: key, secret: "test-oidc-secret", codeNonce: map[string]string{}}
	m.server = httptest.NewServer(http.HandlerFunc(m.serve))
	m.issuer = m.server.URL
	t.Cleanup(m.server.Close)
	return m
}

func (m *miniOIDC) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration"):
		writeJSON(w, map[string]any{
			"issuer":                 m.issuer,
			"authorization_endpoint": m.issuer + "/authorize",
			"token_endpoint":         m.issuer + "/token",
			"jwks_uri":               m.issuer + "/jwks",
		})
	case strings.HasSuffix(r.URL.Path, "/jwks"):
		n := base64.RawURLEncoding.EncodeToString(m.key.PublicKey.N.Bytes())
		writeJSON(w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "t1", "n": n, "e": "AQAB",
			}},
		})
	case strings.HasSuffix(r.URL.Path, "/authorize"):
		q := r.URL.Query()
		const authCode = "test-code"
		if n := q.Get("nonce"); n != "" {
			m.codeNonce[authCode] = n
		}
		redir := q.Get("redirect_uri") + "?code=" + authCode + "&state=" + q.Get("state")
		http.Redirect(w, r, redir, http.StatusFound)
	case strings.HasSuffix(r.URL.Path, "/token"):
		_ = r.ParseForm()
		if r.FormValue("client_id") == "" {
			if user, pass, ok := r.BasicAuth(); ok {
				r.Form.Set("client_id", user)
				r.Form.Set("client_secret", pass)
			}
		}
		now := time.Now()
		nonce := m.codeNonce[r.FormValue("code")]
		if nonce == "" {
			nonce = "fallback-nonce"
		}
		tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": m.issuer, "sub": "oidc-sub-1", "aud": []string{"test-client"},
			"email": "oidc@test.local", "name": "OIDC Test",
			"nonce": nonce,
			"exp":   now.Add(time.Hour).Unix(),
			"iat":   now.Unix(),
		}).SignedString(m.key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"id_token": tok, "access_token": "at"})
	default:
		http.NotFound(w, r)
	}
}

func TestOIDC_ExchangeCode(t *testing.T) {
	idp := startMiniOIDC(t)
	idp.codeNonce["test-code"] = "nonce1"
	t.Setenv("TEST_OIDC_SECRET", idp.secret)
	oidcCfg := auth.OIDCConfig{
		Enabled: true, Issuer: idp.issuer, ClientID: "test-client",
		ClientSecretEnv: "TEST_OIDC_SECRET", RedirectURL: "http://127.0.0.1/callback",
		TenantSlug: "default",
	}
	client, err := auth.NewOIDCClient(context.Background(), oidcCfg)
	require.NoError(t, err)
	_, verifier, err := client.AuthCodeURL("state1", "nonce1")
	require.NoError(t, err)
	claims, err := client.ExchangeCode(context.Background(), "test-code", verifier)
	require.NoError(t, err)
	require.Equal(t, "oidc-sub-1", claims.Sub)
}

func TestOIDC_LoginCallback_IssuesJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	idp := startMiniOIDC(t)
	tid := uuid.New()
	uid := uuid.New()
	cbSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(cbSrv.Close)

	oidcCfg := auth.OIDCConfig{
		Enabled: true, Issuer: idp.issuer, ClientID: "test-client",
		ClientSecretEnv: "TEST_OIDC_SECRET", RedirectURL: cbSrv.URL,
		TenantSlug: "default",
	}
	t.Setenv("TEST_OIDC_SECRET", idp.secret)

	client, err := auth.NewOIDCClient(context.Background(), oidcCfg)
	require.NoError(t, err)
	jwtSvc := auth.NewJWT(auth.JWTConfig{Secret: "test-secret-thirty-two-chars-ok!", TTL: time.Hour})

	h := auth.NewHandler(auth.HandlerDeps{
		LocalEnabled: true,
		Tenants:      fakeTenants{id: tid},
		OIDCUsers: fakeOIDCUsers{user: &user.User{
			ID: uid, TenantID: tid, Email: "oidc@test.local", Role: user.RoleMember,
			OIDCIss: idp.issuer, OIDCSub: "oidc-sub-1",
		}},
		JWT:  jwtSvc,
		OIDC: &auth.OIDCRuntime{Config: oidcCfg, Client: client, CookieSecret: "test-secret-thirty-two-chars-ok!"},
	})
	r := gin.New()
	h.Register(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/oidc/login?tenant=default", nil))
	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "/authorize")
	cookie := w.Header().Get("Set-Cookie")
	require.Contains(t, cookie, "pca_oidc=")

	// Hit the IdP authorize endpoint so nonce is bound to the auth code.
	authResp, err := http.Get(loc)
	require.NoError(t, err)
	_ = authResp.Body.Close()
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	cb := "/auth/oidc/callback?code=test-code&state=" + state
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, cb, nil)
	req2.Header.Set("Cookie", strings.Split(cookie, ";")[0])
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["token"])
}

type fakeOIDCUsers struct {
	user *user.User
	err  error
}

func (f fakeOIDCUsers) FindOrCreateOIDC(_ context.Context, _ uuid.UUID, _, _, _, _ string) (*user.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.user, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
