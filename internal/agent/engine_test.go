package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/orchestrator"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// mockGateway returns a scripted sequence of ChatResponses.
type mockGateway struct {
	responses []*modelgw.ChatResponse
	errs      []error
	calls     []modelgw.ChatRequest
	idx       int
}

func (m *mockGateway) ChatCompletion(_ context.Context, _, _ uuid.UUID,
	req modelgw.ChatRequest) (*modelgw.ChatResponse, error) {
	m.calls = append(m.calls, req)
	if m.idx >= len(m.responses) {
		return nil, errors.New("mockGateway: out of scripted responses")
	}
	resp, err := m.responses[m.idx], error(nil)
	if m.idx < len(m.errs) {
		err = m.errs[m.idx]
	}
	m.idx++
	return resp, err
}

func (m *mockGateway) ChatCompletionStream(_ context.Context, _, _ uuid.UUID,
	req modelgw.ChatRequest, yield func(modelgw.ChatStreamChunk) error) error {
	resp, err := m.ChatCompletion(context.Background(), uuid.Nil, uuid.Nil, req)
	if err != nil {
		return err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return errors.New("mockGateway: empty choices")
	}
	msg := resp.Choices[0].Message
	fr := resp.Choices[0].FinishReason
	const chunkSize = 3
	runes := []rune(msg.Content)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		if err := yield(modelgw.ChatStreamChunk{
			Choices: []modelgw.ChatStreamChoice{{
				Delta: modelgw.ChatStreamDelta{Content: string(runes[i:end])},
			}},
		}); err != nil {
			return err
		}
	}
	if len(msg.ToolCalls) > 0 {
		if err := yield(modelgw.ChatStreamChunk{
			Choices: []modelgw.ChatStreamChoice{{
				Delta:        modelgw.ChatStreamDelta{ToolCalls: msg.ToolCalls},
				FinishReason: &fr,
			}},
		}); err != nil {
			return err
		}
		return nil
	}
	return yield(modelgw.ChatStreamChunk{
		Choices: []modelgw.ChatStreamChoice{{
			FinishReason: &fr,
		}},
	})
}

// mockBus returns canned outputs keyed by tool name.
type mockBus struct {
	tools   []toolbus.ToolDef
	outputs map[string]json.RawMessage
	errs    map[string]error
	calls   []busCall
}

type busCall struct {
	name  string
	input json.RawMessage
}

func (m *mockBus) ListTools(_ context.Context, _ uuid.UUID) []toolbus.ToolDef {
	return m.tools
}

func (m *mockBus) Invoke(_ context.Context, _, _ uuid.UUID,
	name string, input json.RawMessage) (json.RawMessage, error) {
	m.calls = append(m.calls, busCall{name: name, input: input})
	if err, ok := m.errs[name]; ok {
		return nil, err
	}
	if out, ok := m.outputs[name]; ok {
		return out, nil
	}
	return nil, toolbus.ErrToolNotFound
}

func defaultProfileMap() map[string]agent.Profile {
	return map[string]agent.Profile{"coding": agent.DefaultCodingProfile()}
}

func chatStop(text string) *modelgw.ChatResponse {
	return &modelgw.ChatResponse{
		Choices: []modelgw.ChatChoice{{
			Index:        0,
			FinishReason: "stop",
			Message:      modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: text},
		}},
	}
}

func chatToolCall(callID, name, argsJSON string) *modelgw.ChatResponse {
	return &modelgw.ChatResponse{
		Choices: []modelgw.ChatChoice{{
			Index:        0,
			FinishReason: "tool_calls",
			Message: modelgw.ChatMessage{
				Role: modelgw.RoleAssistant,
				ToolCalls: []modelgw.ToolCall{{
					ID:       callID,
					Type:     "function",
					Function: modelgw.ToolCallFunc{Name: name, Arguments: argsJSON},
				}},
			},
		}},
	}
}

func runEngine(t *testing.T, gw agent.Gateway, bus agent.Bus, in agent.RunInput) ([]agent.Event, error) {
	t.Helper()
	if in.ProfileName == "" {
		in.ProfileName = "coding"
	}
	e := agent.NewEngine(gw, bus, defaultProfileMap(), agent.NoopComposer{})
	var events []agent.Event
	err := e.Run(context.Background(), in, func(ev agent.Event) error {
		events = append(events, ev)
		return nil
	})
	return events, err
}

func newRunInput(msg string) agent.RunInput {
	return agent.RunInput{
		TenantID: uuid.New(), UserID: uuid.New(),
		Model:    "default-mock:gpt-4o",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: msg}},
		MaxSteps: 5,
	}
}

func TestEngine_SandboxIDSystemPrefix(t *testing.T) {
	sbID := uuid.MustParse("aaaaaaaa-bbbb-4ccc-dddd-eeeeeeeeeeee")
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	bus := &mockBus{}
	in := newRunInput("hi")
	in.SandboxID = sbID
	_, err := runEngine(t, gw, bus, in)
	require.NoError(t, err)
	require.NotEmpty(t, gw.calls)
	found := false
	for _, m := range gw.calls[0].Messages {
		if m.Role == modelgw.RoleSystem && strings.Contains(m.Content, "Current sandbox_id:") &&
			strings.Contains(m.Content, sbID.String()) {
			found = true
			break
		}
	}
	require.True(t, found, "expected sandbox_id system inject in model messages")
}

func TestEngine_DirectFinal(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("hi back")}}
	bus := &mockBus{}
	events, err := runEngine(t, gw, bus, newRunInput("hi"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(events), 2)
	require.Contains(t, eventKinds(events), agent.EventAssistantDelta)
	require.Equal(t, agent.EventFinal, events[len(events)-1].Kind)
	require.Equal(t, "hi back", events[len(events)-1].Text)
	require.Equal(t, "stop", events[len(events)-1].FinishReason)
	var streamed strings.Builder
	for _, ev := range events {
		if ev.Kind == agent.EventAssistantDelta {
			streamed.WriteString(ev.Text)
		}
	}
	require.Equal(t, "hi back", streamed.String())
}

func TestEngine_StreamYieldsDeltas(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("abcdef")}}
	events, err := runEngine(t, gw, &mockBus{}, newRunInput("hi"))
	require.NoError(t, err)
	var deltas int
	for _, ev := range events {
		if ev.Kind == agent.EventAssistantDelta {
			deltas++
		}
	}
	require.Greater(t, deltas, 1, "mock stream should split text into multiple deltas")
}

func TestEngine_SingleToolCall(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatToolCall("c1", "fs.read", `{"path":"a.txt"}`),
		chatStop("done reading"),
	}}
	bus := &mockBus{
		tools:   []toolbus.ToolDef{{Name: "fs.read", Description: "r", Parameters: json.RawMessage(`{"type":"object"}`)}},
		outputs: map[string]json.RawMessage{"fs.read": json.RawMessage(`{"content":"hello"}`)},
	}
	events, err := runEngine(t, gw, bus, newRunInput("read a.txt"))
	require.NoError(t, err)
	kinds := eventKinds(events)
	require.Equal(t, agent.EventAssistantMessage, kinds[0])
	require.Equal(t, agent.EventToolCall, kinds[1])
	require.Equal(t, agent.EventToolResult, kinds[2])
	require.Contains(t, kinds, agent.EventAssistantDelta)
	require.Equal(t, agent.EventFinal, kinds[len(kinds)-1])
	require.Equal(t, "fs.read", events[1].ToolName)
	require.JSONEq(t, `{"content":"hello"}`, string(events[2].ToolOutput))

	// Bus.Invoke must have been called with the args.
	require.Len(t, bus.calls, 1)
	require.Equal(t, "fs.read", bus.calls[0].name)
	require.JSONEq(t, `{"path":"a.txt"}`, string(bus.calls[0].input))

	// Second LLM call must have the role=tool observation appended.
	require.Len(t, gw.calls, 2)
	last := gw.calls[1].Messages
	require.Equal(t, modelgw.RoleTool, last[len(last)-1].Role)
	require.Equal(t, "c1", last[len(last)-1].ToolCallID)
}

func TestEngine_TwoToolCalls(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatToolCall("c1", "fs.list", `{"path":"."}`),
		chatToolCall("c2", "fs.read", `{"path":"go.mod"}`),
		chatStop("read both"),
	}}
	bus := &mockBus{
		tools: []toolbus.ToolDef{
			{Name: "fs.list", Description: "l", Parameters: json.RawMessage(`{"type":"object"}`)},
			{Name: "fs.read", Description: "r", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
		outputs: map[string]json.RawMessage{
			"fs.list": json.RawMessage(`{"entries":[{"name":"go.mod"}]}`),
			"fs.read": json.RawMessage(`{"content":"module x"}`),
		},
	}
	events, err := runEngine(t, gw, bus, newRunInput("list then read"))
	require.NoError(t, err)
	require.Len(t, bus.calls, 2)
	require.Equal(t, agent.EventFinal, events[len(events)-1].Kind)
}

func TestEngine_BadArgumentsJSON(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatToolCall("c1", "fs.read", `not-json`),
		chatStop("recovered"),
	}}
	bus := &mockBus{
		tools:   []toolbus.ToolDef{{Name: "fs.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
		outputs: map[string]json.RawMessage{"fs.read": json.RawMessage(`{}`)},
	}
	events, err := runEngine(t, gw, bus, newRunInput("read"))
	require.NoError(t, err)
	// Tool was NOT actually invoked.
	require.Len(t, bus.calls, 0)
	// Tool result event present with ToolError set.
	var sawResult bool
	for _, ev := range events {
		if ev.Kind == agent.EventToolResult {
			require.NotEmpty(t, ev.ToolError)
			sawResult = true
		}
	}
	require.True(t, sawResult)
}

func TestEngine_UnknownTool(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatToolCall("c1", "no.such.tool", `{}`),
		chatStop("ok"),
	}}
	bus := &mockBus{
		tools: []toolbus.ToolDef{{Name: "fs.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	events, err := runEngine(t, gw, bus, newRunInput("x"))
	require.NoError(t, err)
	require.Len(t, bus.calls, 0)
	var resultErr string
	for _, ev := range events {
		if ev.Kind == agent.EventToolResult {
			resultErr = ev.ToolError
		}
	}
	require.Contains(t, strings.ToLower(resultErr), "not allowed")
}

func TestEngine_LLMError(t *testing.T) {
	gw := &mockGateway{
		responses: []*modelgw.ChatResponse{nil},
		errs:      []error{modelgw.ErrProviderUnreachable},
	}
	bus := &mockBus{}
	events, err := runEngine(t, gw, bus, newRunInput("hi"))
	require.ErrorIs(t, err, agent.ErrLLMFailed)
	require.NotEmpty(t, events)
	require.Equal(t, agent.EventError, events[len(events)-1].Kind)
}

func TestEngine_MaxStepsExceeded(t *testing.T) {
	// Always return tool_calls so the loop never terminates by itself.
	tc := chatToolCall("c1", "fs.read", `{}`)
	gw := &mockGateway{responses: []*modelgw.ChatResponse{tc, tc, tc, tc}}
	bus := &mockBus{
		tools:   []toolbus.ToolDef{{Name: "fs.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
		outputs: map[string]json.RawMessage{"fs.read": json.RawMessage(`{}`)},
	}
	in := newRunInput("loop")
	in.MaxSteps = 3
	events, err := runEngine(t, gw, bus, in)
	require.ErrorIs(t, err, agent.ErrMaxStepsExceeded)
	require.Equal(t, agent.EventError, events[len(events)-1].Kind)
}

func TestEngine_ToolOutputTruncated(t *testing.T) {
	big := bytes.Repeat([]byte("a"), 60*1024)
	gw := &mockGateway{responses: []*modelgw.ChatResponse{
		chatToolCall("c1", "fs.read", `{"path":"big"}`),
		chatStop("done"),
	}}
	bus := &mockBus{
		tools:   []toolbus.ToolDef{{Name: "fs.read", Parameters: json.RawMessage(`{"type":"object"}`)}},
		outputs: map[string]json.RawMessage{"fs.read": json.RawMessage(big)},
	}
	events, err := runEngine(t, gw, bus, newRunInput("read big"))
	require.NoError(t, err)
	var sawTrunc bool
	for _, ev := range events {
		if ev.Kind == agent.EventToolResult {
			require.True(t, ev.Truncated)
			require.Equal(t, len(big), ev.OriginalSize)
			require.Less(t, len(ev.ToolOutput), len(big))
			sawTrunc = true
		}
	}
	require.True(t, sawTrunc)
}

func TestEngine_EmptyMessages(t *testing.T) {
	gw := &mockGateway{}
	bus := &mockBus{}
	e := agent.NewEngine(gw, bus, defaultProfileMap(), agent.NoopComposer{})
	err := e.Run(context.Background(), agent.RunInput{ProfileName: "coding"}, func(_ agent.Event) error { return nil })
	require.ErrorIs(t, err, agent.ErrEmptyMessages)
}

func TestEngine_UnknownProfile(t *testing.T) {
	gw := &mockGateway{}
	bus := &mockBus{}
	e := agent.NewEngine(gw, bus, defaultProfileMap(), agent.NoopComposer{})
	in := newRunInput("x")
	in.ProfileName = "ghost"
	err := e.Run(context.Background(), in, func(_ agent.Event) error { return nil })
	require.ErrorIs(t, err, agent.ErrUnknownProfile)
}

func eventKinds(events []agent.Event) []agent.EventKind {
	out := make([]agent.EventKind, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.Kind)
	}
	return out
}

// fakeRouter implements orchestrator.Router with a canned Decision. Tests
// can flip injectHint to verify suppression and assert routeCalled to check
// the engine actually invoked the router.
type fakeRouter struct {
	decision    orchestrator.Decision
	injectHint  bool
	routedWith  string // captured UserContent
	routedProf  string // captured Profile
	routeCalled bool
}

func (f *fakeRouter) Route(_ context.Context, in orchestrator.RouteInput) orchestrator.Decision {
	f.routeCalled = true
	f.routedWith = in.UserContent
	f.routedProf = in.Profile
	return f.decision
}
func (f *fakeRouter) InjectHintEnabled() bool { return f.injectHint }

func TestEngine_Router_InjectsHintWhenEnabled(t *testing.T) {
	router := &fakeRouter{
		decision: orchestrator.Decision{
			Matched:   true,
			RuleName:  "marker",
			Type:      "tool",
			Target:    "fs.list",
			Hint:      "ROUTING_HINT_INJECTED",
			MatchedOn: "content_contains",
		},
		injectHint: true,
	}
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	bus := &mockBus{}
	e := agent.NewEngine(gw, bus, defaultProfileMap(), agent.NoopComposer{}).WithRouter(router)
	in := newRunInput("please process this")
	err := e.Run(context.Background(), in, func(_ agent.Event) error { return nil })
	require.NoError(t, err)
	require.True(t, router.routeCalled, "router should have been called")
	require.Equal(t, "please process this", router.routedWith)
	require.Equal(t, "coding", router.routedProf)

	require.NotEmpty(t, gw.calls)
	msgs := gw.calls[0].Messages
	var hintIdx, userIdx int = -1, -1
	for i, m := range msgs {
		if m.Role == modelgw.RoleSystem && m.Content == "ROUTING_HINT_INJECTED" {
			hintIdx = i
		}
		if m.Role == modelgw.RoleUser {
			userIdx = i
		}
	}
	require.NotEqual(t, -1, hintIdx, "hint should be injected as system msg: %+v", msgs)
	require.NotEqual(t, -1, userIdx, "user msg should remain in payload")
	require.Less(t, hintIdx, userIdx, "hint should sit before user message")
}

func TestEngine_Router_HintSuppressedWhenInjectDisabled(t *testing.T) {
	router := &fakeRouter{
		decision: orchestrator.Decision{
			Matched: true, Hint: "SHOULD_NOT_APPEAR",
		},
		injectHint: false,
	}
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	e := agent.NewEngine(gw, &mockBus{}, defaultProfileMap(), agent.NoopComposer{}).WithRouter(router)
	err := e.Run(context.Background(), newRunInput("hi"), func(_ agent.Event) error { return nil })
	require.NoError(t, err)
	require.True(t, router.routeCalled)
	for _, m := range gw.calls[0].Messages {
		require.NotContains(t, m.Content, "SHOULD_NOT_APPEAR")
	}
}

func TestEngine_Router_EmptyHintNotInjectedEvenIfEnabled(t *testing.T) {
	// Baseline: with router=nil we get N system messages (coding profile prompt).
	baselineGW := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	baseEng := agent.NewEngine(baselineGW, &mockBus{}, defaultProfileMap(), agent.NoopComposer{})
	require.NoError(t, baseEng.Run(context.Background(), newRunInput("hi"),
		func(_ agent.Event) error { return nil }))
	baselineSys := 0
	for _, m := range baselineGW.calls[0].Messages {
		if m.Role == modelgw.RoleSystem {
			baselineSys++
		}
	}

	// With a no-match router (and inject_hint=true) we should still see the
	// SAME number of system messages — no extra hint slips through.
	router := &fakeRouter{
		decision:   orchestrator.Decision{Matched: false},
		injectHint: true,
	}
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	e := agent.NewEngine(gw, &mockBus{}, defaultProfileMap(), agent.NoopComposer{}).WithRouter(router)
	require.NoError(t, e.Run(context.Background(), newRunInput("nothing relevant"),
		func(_ agent.Event) error { return nil }))
	require.True(t, router.routeCalled)
	gotSys := 0
	for _, m := range gw.calls[0].Messages {
		if m.Role == modelgw.RoleSystem {
			gotSys++
		}
	}
	require.Equal(t, baselineSys, gotSys, "no extra system message on no-match route")
}

func TestEngine_Router_NilRouterIsNoop(t *testing.T) {
	gw := &mockGateway{responses: []*modelgw.ChatResponse{chatStop("ok")}}
	// Don't call WithRouter at all.
	e := agent.NewEngine(gw, &mockBus{}, defaultProfileMap(), agent.NoopComposer{})
	err := e.Run(context.Background(), newRunInput("hi"), func(_ agent.Event) error { return nil })
	require.NoError(t, err)
}
