package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/auth"
)

// mockRunner returns scripted events / error from Run.
type mockRunner struct {
	events   []agent.Event
	err      error
	got      agent.RunInput
	profiles []agent.Profile
}

func (m *mockRunner) Run(_ context.Context, in agent.RunInput, yield func(agent.Event) error) error {
	m.got = in
	for _, ev := range m.events {
		if err := yield(ev); err != nil {
			return err
		}
	}
	return m.err
}

func (m *mockRunner) Profiles() []agent.Profile { return m.profiles }

func newHandlerRouter(t *testing.T, mr *mockRunner) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	tok, _ := j.Issue(uuid.New(), uuid.New(), "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	agent.NewHandler(mr).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_Run_OK(t *testing.T) {
	mr := &mockRunner{
		events: []agent.Event{
			{Kind: agent.EventAssistantMessage, Step: 1, Text: "hi"},
			{Kind: agent.EventFinal, Step: 1, Text: "hi", FinishReason: "stop"},
		},
	}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"model":    "default-mock:gpt-4o",
		"profile":  "coding",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Events []agent.Event `json:"events"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Events, 2)
	require.Equal(t, agent.EventFinal, resp.Events[1].Kind)
	require.Equal(t, "default-mock:gpt-4o", mr.got.Model)
}

func TestHandler_Run_NoAuth(t *testing.T) {
	mr := &mockRunner{}
	r, _ := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"model":    "x",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Run_BadRequest_NoModel(t *testing.T) {
	mr := &mockRunner{}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "model_required")
}

func TestHandler_Run_BadRequest_NoMessages(t *testing.T) {
	mr := &mockRunner{}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{"model": "x:y"})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "messages_required")
}

func TestHandler_Run_UnknownProfile(t *testing.T) {
	mr := &mockRunner{err: agent.ErrUnknownProfile}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"model":    "x:y",
		"profile":  "ghost",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unknown_profile")
}

func TestHandler_Run_LLMFailed(t *testing.T) {
	mr := &mockRunner{err: agent.ErrLLMFailed}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"model":    "x:y",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "llm_failed")
}

func TestHandler_Run_MaxSteps_Returns200WithError(t *testing.T) {
	mr := &mockRunner{
		events: []agent.Event{{Kind: agent.EventError, Step: 16, Text: "max"}},
		err:    agent.ErrMaxStepsExceeded,
	}
	r, tok := newHandlerRouter(t, mr)
	body, _ := json.Marshal(map[string]any{
		"model":    "x:y",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/agent/run", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "max_steps_exceeded")
	require.Contains(t, w.Body.String(), `"events"`)
}

func TestHandler_ListProfiles_OK(t *testing.T) {
	mr := &mockRunner{profiles: []agent.Profile{
		agent.DefaultCodingProfile(),
		agent.DefaultReviewProfile(),
		agent.DefaultResearchProfile(),
		agent.DefaultWorkflowAuthoringProfile(),
	}}
	r, tok := newHandlerRouter(t, mr)
	req := httptest.NewRequest(http.MethodGet, "/agent/profiles", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Profiles []agent.ProfileInfo `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Profiles, 4)
	names := []string{}
	for _, p := range resp.Profiles {
		names = append(names, p.Name)
		require.NotEmpty(t, p.Description, "profile %s missing description", p.Name)
	}
	require.Contains(t, names, "coding")
	require.Contains(t, names, "review")
	require.Contains(t, names, "research")
	require.Contains(t, names, "workflow-authoring")
}

func TestHandler_ListProfiles_NoAuth(t *testing.T) {
	mr := &mockRunner{}
	r, _ := newHandlerRouter(t, mr)
	req := httptest.NewRequest(http.MethodGet, "/agent/profiles", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
