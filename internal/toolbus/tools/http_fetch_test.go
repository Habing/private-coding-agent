package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestHTTPFetch_DisabledReturnsNil(t *testing.T) {
	require.Nil(t, tools.NewHTTPFetch(tools.HTTPFetchConfig{Enabled: false}))
}

func TestHTTPFetch_AllowlistAndGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello news"))
	}))
	t.Cleanup(srv.Close)

	host := httptestServerHost(t, srv)
	tool := tools.NewHTTPFetch(tools.HTTPFetchConfig{
		Enabled:         true,
		AllowHosts:      []string{host},
		TimeoutSec:      5,
		MaxBodyBytes:    4096,
		BlockPrivateIPs: false,
	})
	require.NotNil(t, tool)

	raw, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(), mustJSON(t, map[string]any{
		"url": srv.URL,
	}))
	require.NoError(t, err)
	var out struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Equal(t, 200, out.StatusCode)
	require.Equal(t, "hello news", out.Body)
}

func TestHTTPFetch_DeniesHostNotInAllowlist(t *testing.T) {
	tool := tools.NewHTTPFetch(tools.HTTPFetchConfig{
		Enabled:    true,
		AllowHosts: []string{"example.com"},
	})
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(), mustJSON(t, map[string]any{
		"url": "https://evil.test/",
	}))
	require.Error(t, err)
}

func TestHTTPFetch_SetAllowHosts(t *testing.T) {
	tool := tools.NewHTTPFetch(tools.HTTPFetchConfig{
		Enabled: true, AllowHosts: []string{"a.example.com"},
	})
	tool.SetAllowHosts([]string{"*.baidu.com"})
	require.Equal(t, []string{"*.baidu.com"}, tool.AllowHosts())
}

func httptestServerHost(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Hostname()
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
