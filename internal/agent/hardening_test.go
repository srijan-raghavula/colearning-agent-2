package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"educore/internal/evaluator"
)

// ---- 1. Tool-calling protocol (wire format) unit tests ---------------------

func TestToolCallingWireFormat(t *testing.T) {
	// Assistant turn with tool_calls must marshal to OpenAI shape:
	// {"role":"assistant","content":"...","tool_calls":[{id,type,function:{name,arguments}}]}
	msg := evaluator.ChatMessage{
		Role:      "assistant",
		Content:   "thinking",
		ToolCalls: json.RawMessage(`[{"id":"call_1","type":"function","function":{"name":"get_assignment_context","arguments":"{}"}}]`),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round["role"] != "assistant" {
		t.Fatalf("role = %v", round["role"])
	}
	if _, ok := round["content"].(string); !ok {
		t.Fatalf("content must be a plain string, got %T (%v)", round["content"], round["content"])
	}
	tcs, ok := round["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("tool_calls missing: %v", round)
	}
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" || tc["type"] != "function" {
		t.Fatalf("tool_call id/type = %v/%v", tc["id"], tc["type"])
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_assignment_context" {
		t.Fatalf("function.name = %v", fn["name"])
	}
}

func TestToolObservationMessageCarriesToolCallID(t *testing.T) {
	msg := evaluator.ChatMessage{Role: "tool", ToolCallID: "call_1", Content: `{"ok":true}`}
	b, _ := json.Marshal(msg)
	var round map[string]any
	_ = json.Unmarshal(b, &round)
	if round["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id lost: %v", round)
	}
	if round["role"] != "tool" {
		t.Fatalf("role = %v", round["role"])
	}
}

func TestExtractToolCallsHandlesParallelCalls(t *testing.T) {
	msg := evaluator.ChatMessage{
		Role: "assistant",
		ToolCalls: json.RawMessage(`[
			{"id":"c1","type":"function","function":{"name":"search_submission","arguments":"{\"query\":\"a\"}"}},
			{"id":"c2","type":"function","function":{"name":"search_submission","arguments":"{\"query\":\"b\"}"}}
		]`),
	}
	calls, _, err := evaluator.ExtractToolCalls(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0].Name != "search_submission" || calls[1].ID != "c2" {
		t.Fatalf("parallel tool calls mangled: %+v", calls)
	}
}

func TestChatTurnWireShape(t *testing.T) {
	// Verify the exact request body sent to the provider matches the
	// OpenAI-compatible contract DeepInfra expects.
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := &evaluator.Provider{BaseURL: srv.URL, APIKey: "sk-x", Model: "m1"}
	msgs := []evaluator.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "", ToolCalls: json.RawMessage(`[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]`)},
		{Role: "tool", ToolCallID: "c1", Content: "obs"},
		{Role: "user", Content: "next"},
	}
	_, err := p.ChatTurn(context.Background(), msgs, nil, 64)
	if err != nil {
		t.Fatal(err)
	}
	ml, ok := gotBody["messages"].([]any)
	if !ok || len(ml) != 4 {
		t.Fatalf("messages shape: %v", gotBody)
	}
	fields := ml[1].(map[string]any)
	if _, isStr := fields["content"].(string); !isStr {
		t.Fatalf("assistant content must be a string on the wire, got: %v", fields)
	}
	if _, hasTc := fields["tool_calls"]; !hasTc {
		t.Fatalf("assistant tool_calls missing from wire: %v", fields)
	}
	third := ml[2].(map[string]any)
	if third["tool_call_id"] != "c1" {
		t.Fatalf("tool message tool_call_id lost on wire: %v", third)
	}
}

// ---- 2. Budget exhaustion ---------------------------------------------------

func TestBudgetToolCallCapStopsCleanly(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("a", ToolGetContext, map[string]any{}) },
		func(_ *http.Request) string { return toolCallJSON("b", ToolGetContext, map[string]any{}) },
		func(_ *http.Request) string { return toolCallJSON("c", ToolGetContext, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	eng.MaxIter = 12
	eng.MaxToolCalls = 2 // only 2 tool executions allowed
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 12)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed || !strings.Contains(run.Error, "tool call budget exceeded") {
		t.Fatalf("tool budget not enforced: status=%s err=%q", run.Status, run.Error)
	}
	if run.ResultEvaluationID != 0 {
		t.Fatal("budget exhaustion must not produce an evaluation")
	}
	// state persisted
	var messages string
	_ = st.DB.QueryRow(`SELECT messages FROM agent_runs WHERE id = ?`, runID).Scan(&messages)
	if messages == "" || messages == "[]" {
		t.Fatal("durable state lost on budget stop")
	}
}

func TestBudgetOutputCharCapStopsCleanly(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("a", ToolInspect, map[string]any{}) },
		func(_ *http.Request) string { return toolCallJSON("b", ToolInspect, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	eng.MaxOutChars = 100 // tiny budget
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 12)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed || !strings.Contains(run.Error, "output character budget exceeded") {
		t.Fatalf("output budget not enforced: status=%s err=%q", run.Status, run.Error)
	}
	if run.ResultEvaluationID != 0 {
		t.Fatal("output budget exhaustion must not produce an evaluation")
	}
}

func TestBudgetDurationCapStopsCleanly(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("a", ToolGetContext, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, -time.Minute) // negative → already exceeded
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 12)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed || !strings.Contains(run.Error, "total execution budget exceeded") {
		t.Fatalf("duration budget not enforced: status=%s err=%q", run.Status, run.Error)
	}
	if run.ResultEvaluationID != 0 {
		t.Fatal("duration budget exhaustion must not produce an evaluation")
	}
}

// ---- 3. Follow-up restrictions ----------------------------------------------

func TestFollowUpExcludesAllMutatingTools(t *testing.T) {
	tools := followUpTools()
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.Name] = true
	}
	for _, mutating := range []string{ToolEvaluateCrit, ToolVerify, ToolSave} {
		if names[mutating] {
			t.Fatalf("read-only follow-up exposes mutating tool %s", mutating)
		}
	}
	for _, safe := range []string{ToolGetContext, ToolInspect, ToolSearch} {
		if !names[safe] {
			t.Fatalf("read-only follow-up missing evidence tool %s", safe)
		}
	}
}

func TestFollowUpEvidenceGroundedAnswer(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("f", ToolSearch, map[string]any{"query": "recursive descent"})
		},
		func(_ *http.Request) string {
			return textJSON("The submission states 'Recursive descent uses one function per non-terminal' (offset 0).")
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindFollowup, true, 4)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed {
		// follow-ups end at the iteration cap without save; final answer is in the trace
		t.Fatalf("status=%s err=%q", run.Status, run.Error)
	}
	// the answer must be grounded in actual evidence (search tool ran)
	found := false
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolResult && strings.Contains(e.Payload, "character offset") {
			found = true
		}
	}
	if !found {
		t.Fatal("follow-up run did not gather evidence via search_submission")
	}
	var approved int
	_ = st.DB.QueryRow(`SELECT COUNT(1) FROM evaluations`).Scan(&approved)
	if approved != 0 {
		t.Fatal("follow-up run created evaluations")
	}
}

func TestFollowUpCannotApproveOrPublish(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	// even if the model "asks" for a mutating tool by name, the registry excludes it
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("s", ToolSave, map[string]any{"confidence": "HIGH"}) },
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{"criterion_id": 1})
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindFollowup, true, 3)
	drainAll(t, eng)
	var approved, total int
	_ = st.DB.QueryRow(`SELECT COUNT(1), COALESCE(SUM(CASE WHEN approved=1 THEN 1 ELSE 0 END),0) FROM evaluations`).Scan(&total, &approved)
	if total != 0 || approved != 0 {
		t.Fatalf("follow-up mutated evaluations: total=%d approved=%d", total, approved)
	}
	// mutating tool attempts were rejected as unknown
	rejected := false
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolError && (strings.Contains(e.Payload, "unknown tool") || strings.Contains(e.Payload, ToolSave)) {
			rejected = true
		}
	}
	if !rejected {
		t.Fatal("mutating tool attempt not rejected in trace")
	}
}
