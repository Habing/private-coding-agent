// mockoidc is a minimal OIDC provider for compose E2E (slice 15).
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var codeNonces sync.Map // auth code -> nonce (in-memory stub IdP session)

func main() {
	issuer := env("MOCK_OIDC_ISSUER", "http://mock-oidc:8082")
	public := strings.TrimRight(env("MOCK_OIDC_PUBLIC_URL", "http://localhost:8082"), "/")
	clientSecret := env("MOCK_OIDC_CLIENT_SECRET", "e2e-oidc-secret-change-me")
	clientID := env("MOCK_OIDC_CLIENT_ID", "pca-e2e")
	port := env("MOCK_OIDC_PORT", "8082")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	jwksJSON, err := rsaJWKS(&key.PublicKey)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                public + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		redirectURI := r.URL.Query().Get("redirect_uri")
		state := r.URL.Query().Get("state")
		if redirectURI == "" || state == "" {
			http.Error(w, "redirect_uri and state required", http.StatusBadRequest)
			return
		}
		const authCode = "e2e-oidc-code"
		if n := r.URL.Query().Get("nonce"); n != "" {
			codeNonces.Store(authCode, n)
		}
		sep := "?"
		if strings.Contains(redirectURI, "?") {
			sep = "&"
		}
		http.Redirect(w, r, redirectURI+sep+"code="+authCode+"&state="+state, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "unsupported grant_type", http.StatusBadRequest)
			return
		}
		formID := r.FormValue("client_id")
		formSecret := r.FormValue("client_secret")
		if formID == "" || formSecret == "" {
			user, pass, ok := r.BasicAuth()
			if ok {
				formID, formSecret = user, pass
			}
		}
		if formID != clientID {
			http.Error(w, "invalid client", http.StatusUnauthorized)
			return
		}
		if formSecret != clientSecret {
			http.Error(w, "invalid secret", http.StatusUnauthorized)
			return
		}
		nonce := "e2e-nonce"
		if v, ok := codeNonces.Load(r.FormValue("code")); ok {
			nonce = v.(string)
		}
		idTok, err := signIDToken(key, issuer, clientID, nonce)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idTok,
		})
	})

	addr := ":" + port
	log.Printf("mock-oidc listening on %s issuer=%s public=%s", addr, issuer, public)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func signIDToken(key *rsa.PrivateKey, iss, aud, nonce string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": iss, "sub": "e2e-oidc-user", "aud": []string{aud},
		"email": "oidc-e2e@example.com", "name": "OIDC E2E User",
		"nonce": nonce,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
}

func rsaJWKS(pub *rsa.PublicKey) ([]byte, error) {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := encodeExponent(pub.E)
	doc := map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "mock-e2e",
			"n": n, "e": e,
		}},
	}
	return json.Marshal(doc)
}

func encodeExponent(e int) string {
	if e == 65537 {
		return "AQAB"
	}
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(e))
	return base64.RawURLEncoding.EncodeToString(buf[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
