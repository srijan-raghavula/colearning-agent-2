package evaluator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func baseInput() Input {
	return Input{
		AssignmentTitle: "Graph Traversal",
		Instructions:    "Implement and explain BFS and DFS.",
		Topic:           "Graphs",
		SubmissionText: `Breadth-first search explores a graph level by level using a queue.
Depth-first search uses a stack and follows each branch as deep as possible
before backtracking. Both algorithms visit all reachable vertices exactly once.`,
		Criteria: []CriterionInput{
			{ID: 1, Title: "Correctness", Description: "right algo", MaxPoints: 60, Keywords: "queue,stack,level"},
			{ID: 2, Title: "Complexity", Description: "analysis", MaxPoints: 40, Keywords: "vertices"},
		},
		ExtractionOK: true,
	}
}

func validOuts() []CriterionResultOut {
	return []CriterionResultOut{
		{CriterionID: 1, AwardedPoints: 46.504, Evidence: []Evidence{{Quote: "level by level using a queue", Explanation: "Student describes BFS queue behavior"}}, Feedback: "Describes both traversal orders concretely."},
		{CriterionID: 2, AwardedPoints: 20, Evidence: []Evidence{{Quote: "visit all reachable vertices exactly once", Explanation: "Implies O(V+E) reasoning"}}, Feedback: "Implies complexity without stating it."},
	}
}

func TestValidateHappyPath(t *testing.T) {
	res, err := validateResult(baseInput(), validOuts(), "Solid traversal summary", []string{"covers BFS and DFS"}, []string{"no complexity stated"}, []string{"state O(V+E)"}, ConfMedium, "clear quotes", MethodAI)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if res.TotalScore != 66.50 {
		t.Fatalf("total = %.2f, want 66.50 (rounded)", res.TotalScore)
	}
	if res.Criteria[0].AwardedPoints != 46.50 {
		t.Fatalf("rounding: %.4f", res.Criteria[0].AwardedPoints)
	}
	if res.NeedsReview {
		t.Fatalf("strong result should not need review: %v", res.Warnings)
	}
}

func TestValidateMissingDuplicateUnknown(t *testing.T) {
	// missing criterion 2
	if _, err := validateResult(baseInput(), validOuts()[:1], "x", nil, nil, nil, ConfHigh, "", MethodAI); err == nil {
		t.Fatal("missing criterion accepted")
	}
	// duplicate
	in := baseInput()
	in.Criteria = append(in.Criteria, CriterionInput{ID: 1, Title: "Dup", MaxPoints: 1})
	if _, err := validateResult(in, validOuts(), "x", nil, nil, nil, "", "", MethodAI); err == nil {
		t.Fatal("duplicate input criterion accepted")
	}
	// unknown ID
	outs := append(validOuts(), CriterionResultOut{CriterionID: 99, AwardedPoints: 5})
	if _, err := validateResult(baseInput(), outs, "x", nil, nil, nil, "", "", MethodAI); err == nil {
		t.Fatal("unknown criterion accepted")
	}
	// dup response criterion
	dup := []CriterionResultOut{validOuts()[0], validOuts()[0], validOuts()[1]}
	if _, err := validateResult(baseInput(), dup, "x", nil, nil, nil, "", "", MethodAI); err == nil {
		t.Fatal("duplicate response criterion accepted")
	}
}

func TestValidateClampsAndScores(t *testing.T) {
	outs := []CriterionResultOut{
		{CriterionID: 1, AwardedPoints: -5},  // clamp to 0
		{CriterionID: 2, AwardedPoints: 140}, // clamp to max 40
	}
	res, err := validateResult(baseInput(), outs, "x", nil, nil, nil, ConfHigh, "", MethodAI)
	if err != nil {
		t.Fatalf("clamp validation: %v", err)
	}
	if res.TotalScore != 40 {
		t.Fatalf("clamped total = %.2f", res.TotalScore)
	}
	if len(res.Warnings) < 2 {
		t.Fatalf("clamp warnings missing: %+v", res.Warnings)
	}
}

func TestEvidenceVerification(t *testing.T) {
	outs := []CriterionResultOut{
		{CriterionID: 1, AwardedPoints: 30, Evidence: []Evidence{
			{Quote: "level by level using a queue", Explanation: "ok"},       // real
			{Quote: "uses a hash table for storage", Explanation: "made up"}, // absent
		}},
		{CriterionID: 2, AwardedPoints: 10},
	}
	res, err := validateResult(baseInput(), outs, "x", nil, nil, nil, ConfLow, "", MethodAI)
	if err != nil {
		t.Fatal(err)
	}
	f := res.Criteria[0]
	if len(f.Evidence) != 1 || len(f.DroppedEvidence) != 1 {
		t.Fatalf("evidence split wrong: kept=%d dropped=%d", len(f.Evidence), len(f.DroppedEvidence))
	}
	// Whitespace normalization stays substring-safe.
	if !quoteContains(baseInput().SubmissionText, "level by level\nUSING A QUEUE") {
		t.Fatal("normalized whitespace matching broken")
	}
	if quoteContains("abc", "abcd") || quoteContains("abc", "xyz") || quoteContains("", "  ") {
		t.Fatal("invented quote risk")
	}
}

func TestDemoModeLabelsAndScores(t *testing.T) {
	res, err := DemoEvaluate(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.Method != MethodDemo || res.Confidence != ConfLow || !res.NeedsReview {
		t.Fatalf("demo labeling: %s %s needsReview=%v", res.Method, res.Confidence, res.NeedsReview)
	}
	if !strings.Contains(res.OverallFeedback, "DEMO") && !strings.Contains(res.ConfidenceReason, "faculty") {
		t.Fatalf("demo disclosure missing: %q", res.OverallFeedback)
	}
	// keywords queue,stack,level,vertices all present → full coverage expected
	if len(res.Criteria[0].Evidence) == 0 {
		t.Fatal("demo produced no evidence")
	}
	var total float64
	for _, c := range res.Criteria {
		total += c.AwardedPoints
	}
	if total != res.TotalScore {
		t.Fatalf("demo totals mismatch: %f vs %f", total, res.TotalScore)
	}
}

var _ = json.Valid

// ---------- Provider adapter (fake HTTP server only — no real vendor calls) ----------

func fakeProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Provider{BaseURL: srv.URL, APIKey: "sk-test", Model: "test-model", HTTP: srv.Client()}
}

func TestProviderMalformedAndErrors(t *testing.T) {
	// malformed JSON
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not json"))
	})
	if _, err := p.Complete(context.Background(), "s", "u", 16); err == nil {
		t.Fatal("malformed response accepted")
	}
	// HTTP error
	p = fakeProvider(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusBadGateway) })
	if _, err := p.Complete(context.Background(), "s", "u", 16); err == nil {
		t.Fatal("HTTP error accepted")
	}
	// no choices
	p = fakeProvider(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) })
	if _, err := p.Complete(context.Background(), "s", "u", 16); err == nil {
		t.Fatal("empty choices accepted")
	}
}

func TestProviderTimeout(t *testing.T) {
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	})
	p.HTTP = &http.Client{Timeout: 20 * time.Millisecond}
	if _, err := p.Complete(context.Background(), "s", "u", 16); err == nil {
		t.Fatal("timeout not surfaced")
	}
}

func TestProviderRequestAuthAndPayload(t *testing.T) {
	var gotAuth, gotModel string
	var temp float64
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotModel = req.Model
		temp = req.Temperature
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{}"}}]}`))
	})
	if _, err := p.Complete(context.Background(), "sys", "user", 128); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth header wrong: %q", gotAuth)
	}
	if gotModel != "test-model" || temp != 0.2 {
		t.Fatalf("payload wrong: model=%s temp=%f", gotModel, temp)
	}
}

func TestEngineEmptyAndExtractionFailures(t *testing.T) {
	e := &Engine{}
	in := baseInput()
	in.SubmissionText = ""
	if _, err := e.Evaluate(context.Background(), in); !errors.Is(err, errNoText) {
		t.Fatalf("empty submission accepted: %v", err)
	}
	in.ExtractionOK = false
	in.SubmissionText = "some fallback text"
	res, err := e.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsReview {
		_ = res
	}
}

func TestEngineAIParse(t *testing.T) {
	in := baseInput()
	payload, _ := json.Marshal(map[string]any{
		"criteria": []map[string]any{
			{"criterion_id": 1, "awarded_points": 45, "evidence": []map[string]string{{"quote": "queue", "explanation": "present"}}},
			{"criterion_id": 2, "awarded_points": 20},
		},
		"overall_feedback":       "Readable answer",
		"strengths":              []string{"names both algorithms"},
		"learning_gaps":          []string{},
		"suggested_improvements": []string{"include complexity"},
		"confidence":             "MEDIUM",
		"confidence_reason":      "single short answer",
	})
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(string(payload)) + `}}]}`))
	})
	e := &Engine{Provider: p}
	res, err := e.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalScore != 65 || res.Method != MethodAI {
		t.Fatalf("wrong result: total=%.2f method=%s", res.TotalScore, res.Method)
	}
	if len(res.Criteria[1].Evidence) != 0 {
		t.Fatal("no evidence must be okay too")
	}
}

// quoteJSON wraps a raw JSON string as a JSON string literal.
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
