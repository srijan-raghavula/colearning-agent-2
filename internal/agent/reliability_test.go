package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"educore/internal/evaluator"
	"educore/internal/store"
)

func mustRun(t *testing.T, st *store.Store, fac, sub int64, kind string, readonly bool, maxIter int) int64 {
	t.Helper()
	id, err := st.CreateAgentRun(sub, fac, kind, "", maxIter, readonly)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func drainAll(t *testing.T, eng *Engine) {
	t.Helper()
	for eng.ProcessNext(context.Background()) {
	}
}

func eventsOf(t *testing.T, st *store.Store, runID int64) []*store.AgentEvent {
	t.Helper()
	ev, err := st.AgentEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

var _ = json.Marshal

func TestAgentInvalidToolArgumentsRecovered(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("a", ToolEvaluateCrit, map[string]any{"criterion_id": 1, "awarded_points": 900, "evidence": []map[string]string{}, "feedback": "x"})
		},
		func(_ *http.Request) string { return toolCallJSON("b", ToolGetContext, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 3)
	drainAll(t, eng)
	ev := eventsOf(t, st, runID)
	foundErr := false
	for _, e := range ev {
		if e.Kind == EventToolError && strings.Contains(e.Payload, "exceeds max_points") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatalf("invalid args error not traced: %+v", ev)
	}
	// run continues (failure not fatal), escalates at cap without save
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed {
		t.Fatalf("expected FAILED escalation at cap, got %s", run.Status)
	}
	if run.ResultEvaluationID != 0 {
		t.Fatal("no evaluation may be saved after failed tool validation")
	}
}

func TestAgentUnknownToolRejected(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("u", "drop_database", map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, true, 2)
	drainAll(t, eng)
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolError && strings.Contains(e.Payload, "unknown tool drop_database") {
			return
		}
	}
	t.Fatal("unknown tool not rejected in trace")
}

func TestAgentFabricatedEvidenceRejected(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": 50,
				"evidence": []map[string]string{{"quote": "the parser is O(n log n) and uses a hash jump table", "explanation": "invented"}},
				"feedback": "fabricated",
			})
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 2)
	drainAll(t, eng)
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolError && strings.Contains(e.Payload, "no evidence quote found") {
			return
		}
	}
	t.Fatal("fabricated evidence accepted")
}

func TestAgentAuthorizationBoundary(t *testing.T) {
	st, fac, stu, subID := agentFixture(t)
	// a different faculty member cannot start runs on another's submission
	var foreign int64
	_ = st.DB.QueryRow(`INSERT INTO users (email,name,password_hash,role) VALUES ('other@x.edu','Other','x','FACULTY') RETURNING id`).Scan(&foreign)
	if _, err := st.CreateAgentRun(subID, fac, KindEvaluate, "", 3, false); err != nil {
		t.Fatal(err)
	}
	// run detail resolves via ownership check
	_, err := st.AgentRunByID(mustLatestRun(st), foreign)
	if err != store.ErrForbidden {
		t.Fatalf("foreign faculty admitted: %v", err)
	}
	_, err = st.AgentRunByID(mustLatestRun(st), fac)
	if err != nil {
		t.Fatalf("owner denied: %v", err)
	}
	_ = stu
}

func mustLatestRun(st *store.Store) int64 {
	var id int64
	_ = st.DB.QueryRow(`SELECT id FROM agent_runs ORDER BY id DESC LIMIT 1`).Scan(&id)
	return id
}

func TestAgentMaxIterationsEnforced(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	// model loops forever calling verify only
	var calls int32
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			atomic.AddInt32(&calls, 1)
			return toolCallJSON("v", ToolVerify, map[string]any{})
		},
		func(_ *http.Request) string {
			atomic.AddInt32(&calls, 1)
			return toolCallJSON("v", ToolVerify, map[string]any{})
		},
		func(_ *http.Request) string {
			atomic.AddInt32(&calls, 1)
			return toolCallJSON("v", ToolVerify, map[string]any{})
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	eng.MaxIter = 3
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 3)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed || run.Iterations < 3 {
		t.Fatalf("max iterations not enforced: status=%s iter=%d calls=%d", run.Status, run.Iterations, calls)
	}
	if run.ResultEvaluationID != 0 {
		t.Fatal("iteration cap must not produce an evaluation")
	}
}

func TestAgentFollowUpReadOnly(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("f", ToolSave, map[string]any{"overall_feedback": "x", "strengths": []string{}, "learning_gaps": []string{}, "suggested_improvements": []string{}, "confidence": "LOW", "confidence_reason": "y"})
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindFollowup, true, 2)
	drainAll(t, eng)
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolError && strings.Contains(e.Payload, "unknown tool") {
			return // mutating tools are excluded from read-only runs
		}
	}
	t.Fatal("follow-up conversation mutated marks")
	var approved int
	_ = st.DB.QueryRow(`SELECT COUNT(1) FROM evaluations WHERE approved = 1`).Scan(&approved)
	if approved != 0 {
		t.Fatal("follow-up published marks")
	}
}

func TestAgentReconsiderSavesNewAttempt(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	// Baseline: evaluate→save (attempt 1, unapproved). Then reconsider run.
	_, p2 := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": 40,
				"evidence": []map[string]string{{"quote": "one function per non-terminal", "explanation": "grammar mapping"}},
				"feedback": "base",
			})
		},
		func(_ *http.Request) string {
			return toolCallJSON("e2", ToolEvaluateCrit, map[string]any{
				"criterion_id": 2, "awarded_points": 30,
				"evidence": []map[string]string{{"quote": "LLVM is a compiler", "explanation": "toolchain reference"}},
				"feedback": "base",
			})
		},
		func(_ *http.Request) string {
			return toolCallJSON("s", ToolSave, map[string]any{"overall_feedback": "base", "strengths": []string{}, "learning_gaps": []string{}, "suggested_improvements": []string{}, "confidence": "MEDIUM", "confidence_reason": "baseline"})
		},
	})
	eng2 := NewEngine(st, p2, 512, 0)
	baseRun := mustRun(t, st, fac, subID, KindEvaluate, false, 4)
	for eng2.ProcessNext(context.Background()) {
	}
	base, _ := st.AgentRunByID(baseRun, fac)
	t.Logf("baseline: status=%s err=%q", base.Status, base.Error)
	if base.ResultEvaluationID == 0 {
		t.Fatal("baseline run did not save")
	}

	// reconsider run: re-evaluates criterion 1 with higher score and saves attempt 2
	_, p3 := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": 55,
				"evidence": []map[string]string{{"quote": "one function per non-terminal", "explanation": "better mapping insight"}},
				"feedback": "reconsidered",
			})
		},
		func(_ *http.Request) string {
			return toolCallJSON("e2", ToolEvaluateCrit, map[string]any{
				"criterion_id": 2, "awarded_points": 33,
				"evidence": []map[string]string{{"quote": "LLVM is a compiler", "explanation": "toolchain reference again"}},
				"feedback": "revised",
			})
		},
		func(_ *http.Request) string {
			return toolCallJSON("s", ToolSave, map[string]any{"overall_feedback": "revised", "strengths": []string{}, "learning_gaps": []string{}, "suggested_improvements": []string{}, "confidence": "MEDIUM", "confidence_reason": "reconsideration"})
		},
	})
	eng3 := NewEngine(st, p3, 512, 0)
	recRun := mustRun(t, st, fac, subID, KindReconsider, false, 4)
	for eng3.ProcessNext(context.Background()) {
	}
	r2, _ := st.AgentRunByID(recRun, fac)
	if r2.ResultEvaluationID == 0 || false {
		t.Fatalf("reconsider must create a NEW attempt (got %d)", r2.ResultEvaluationID)
	}
	var attempts int
	_ = st.DB.QueryRow(`SELECT COUNT(1) FROM evaluations WHERE submission_id = ?`, subID).Scan(&attempts)
	if attempts != 2 {
		t.Fatalf("attempt history broken: %d", attempts)
	}
}

func TestAgentRunStatePersistsAndResumes(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return toolCallJSON("c", ToolGetContext, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 9)
	// Turn 1 executes and checkpoints into durable run state. The cap
	// eventually fails the run (no save), which is fine for this check.
	eng.ProcessNext(context.Background())
	var messages, draft string
	_ = st.DB.QueryRow(`SELECT messages, draft FROM agent_runs WHERE id = ?`, runID).Scan(&messages, &draft)
	if !strings.Contains(messages, "system") {
		t.Fatal("msg missing")
	}
	if draft == "{}" {
		t.Fatal("draft empty")
	}
	// simulate a crash mid-run: mark RUNNING, then restart-time recovery.
	if _, err := st.DB.Exec(`UPDATE agent_runs SET status = 'RUNNING' WHERE id = ?`, runID); err != nil {
		t.Fatal(err)
	}
	n, err := st.RecoverAgentRuns()
	if err != nil || n < 1 {
		t.Fatalf("recover: n=%d err=%v", n, err)
	}
	var status string
	_ = st.DB.QueryRow(`SELECT status FROM agent_runs WHERE id = ?`, runID).Scan(&status)
	if status != StatusPending {
		t.Fatalf("run not resumable: %s", status)
	}
}

func TestAgentDraftPersistsAcrossTurns(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": 40,
				"evidence": []map[string]string{{"quote": "one function per non-terminal", "explanation": "grammar"}},
				"feedback": "ok",
			})
		},
		func(_ *http.Request) string { return toolCallJSON("g", ToolGetContext, map[string]any{}) },
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 2)
	drainAll(t, eng)
	var draft string
	_ = st.DB.QueryRow(`SELECT draft FROM agent_runs WHERE id = ?`, runID).Scan(&draft)
	if !strings.Contains(draft, `"awarded_points"`) || !strings.Contains(draft, "one function per non-terminal") {
		t.Fatalf("draft not checkpointed: %s", draft)
	}
}

func TestEvidenceVerificationBounds(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string {
			return toolCallJSON("e", ToolEvaluateCrit, map[string]any{
				"criterion_id": 1, "awarded_points": -5,
				"evidence": []map[string]string{{"quote": "one function per non-terminal", "explanation": "neg"}},
				"feedback": "neg",
			})
		},
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 2)
	drainAll(t, eng)
	negativeGated := false
	for _, e := range eventsOf(t, st, runID) {
		if e.Kind == EventToolError && strings.Contains(e.Payload, "cannot be negative") {
			negativeGated = true
		}
	}
	if !negativeGated {
		t.Fatal("negative score accepted by evaluate_criterion")
	}
	_ = fac
}

func TestAgentProviderFailureMarksFailedKeepsSubmission(t *testing.T) {
	st, fac, _, subID := agentFixture(t)
	_, prov := newFakeModel(t, []func(*http.Request) string{
		func(_ *http.Request) string { return `{"error":{"message":"quota exceeded"}}` },
	})
	eng := NewEngine(st, prov, 512, 0)
	runID := mustRun(t, st, fac, subID, KindEvaluate, false, 2)
	drainAll(t, eng)
	run, _ := st.AgentRunByID(runID, fac)
	if run.Status != StatusFailed || run.Error == "" {
		t.Fatalf("provider failure must fail the run with a message: %+v", run)
	}
	// submission untouched
	var content string
	_ = st.DB.QueryRow(`SELECT content FROM submissions WHERE id = ?`, subID).Scan(&content)
	if content == "" {
		t.Fatal("submission content lost")
	}
	var approved int
	_ = st.DB.QueryRow(`SELECT COUNT(1) FROM evaluations`).Scan(&approved)
	if approved != 0 {
		t.Fatal("failed agent must not persist any evaluation")
	}
	var _ = evaluator.Evidence{}
}
