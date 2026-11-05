package agent

import (
	"context"
	"net/http"
	"testing"

	"educore/internal/evaluator"
	"educore/internal/store"
)

// StoreShim narrows the store surface used by tests.
type StoreShim = store.Store

// Full happy path: the model walks context → inspect → search → evaluate×2 →
// verify → save and the run persists a pending evaluation plus a complete trace.
func TestAgentFullHappyPath(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("t1", ToolGetContext, map[string]any{}) },
		func(_ *http.Request) string { return toolCallJSON("t2", ToolInspect, map[string]any{}) },
		func(_ *http.Request) string {
			return toolCallJSON("t3", ToolSearch, map[string]any{"query": "recursive descent"})
		},
		func(_ *http.Request) string {
			return toolCallJSON("t4", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": 48,
				"evidence": []map[string]string{{"quote": "uses one function per non-terminal", "explanation": "the rule set is directly mapped"}},
				"feedback": "Solid grammar mapping.",
			})
		},
		func(_ *http.Request) string {
			return toolCallJSON("t5", ToolEvaluateCrit, map[string]any{
				"criterion_id": 2, "awarded_points": 30,
				"evidence": []map[string]string{{"quote": "one function per non-terminal", "explanation": "single-pass structure"}},
				"feedback": "Complexity not argued.",
			})
		},
		func(_ *http.Request) string { return toolCallJSON("t6", ToolVerify, map[string]any{}) },
		func(_ *http.Request) string {
			return toolCallJSON("t7", ToolSave, map[string]any{
				"overall_feedback":       "Decent parser summary.",
				"strengths":              []string{"clear grammar mapping"},
				"learning_gaps":          []string{"no complexity analysis"},
				"suggested_improvements": []string{"derive the recursion relations"},
				"confidence":             "MEDIUM",
				"confidence_reason":      "short but consistent answer",
			})
		},
		func(_ *http.Request) string {
			return textJSON("Saved the pending evaluation for your review.")
		},
	})

	eng := NewEngine(st, prov, 512, 0)
	runID, err := st.CreateAgentRun(subID, fac, KindEvaluate, "", DefaultMaxIterations, false)
	if err != nil {
		t.Fatal(err)
	}
	for eng.ProcessNext(context.Background()) {
	}
	run, err := st.AgentRunByID(runID, fac)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusDone {
		t.Fatalf("run status = %s err=%q", run.Status, run.Error)
	}
	if run.ResultEvaluationID == 0 {
		t.Fatal("no evaluation persisted by save tool")
	}
	for _, e := range debugEvents(t, st, runID) {
		t.Logf("EV %2d %-11s %.120s", e.Seq, e.Kind, e.Payload)
	}
	ev2, err := st.EvaluationAttemptByID(run.ResultEvaluationID)
	if err != nil {
		t.Fatal(err)
	}
	if ev2.Approved {
		t.Fatal("agent save must never auto-approve")
	}
	if ev2.Method != evaluator.MethodAI {
		t.Fatalf("method = %s", ev2.Method)
	}
	if ev2.EffectiveTotal != 78 {
		t.Fatalf("total = %.2f, want 78", ev2.EffectiveTotal)
	}
	// trace: every kind present
	events, _ := st.AgentEvents(runID)
	kinds := map[string]int{}
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	for _, want := range []string{EventModelCall, EventToolCall, EventToolResult, EventFinal} {
		if kinds[want] == 0 {
			t.Fatalf("trace missing %s: %+v", want, kinds)
		}
	}
	if msgs := messageCount(t, st, runID); msgs < 8 {
		t.Fatalf("transcript not checkpointed (%d msgs)", msgs)
	}
}

func messageCount(t *testing.T, st *StoreShim, runID int64) int {
	t.Helper()
	var n int
	if err := st.DB.QueryRow(`SELECT json_array_length(messages) FROM agent_runs WHERE id = ?`, runID).Scan(&n); err != nil {
		return 0
	}
	return n
}
