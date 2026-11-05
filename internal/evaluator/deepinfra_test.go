package evaluator

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func fullEvalJSON() string {
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
	return string(payload)
}

func TestDeepInfraSuccessParsing(t *testing.T) {
	var calls int32
	var auth string
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/v1/openai/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` + quoteJSON(fullEvalJSON()) + `}}]}`))
	})
	p.BaseURL += "/v1/openai"
	e := &Engine{Provider: p}
	in := baseInput()
	res, err := e.Evaluate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("success path must cost exactly one request, got %d", got)
	}
	if auth != "Bearer sk-test" {
		t.Fatalf("bearer header missing")
	}
	if res.TotalScore != 65 || res.Method != MethodAI {
		t.Fatalf("bad parse: %+v", res)
	}
}

func TestDeepInfraAuthFailureNoRetryNoFallback(t *testing.T) {
	var calls int32
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":{"message":"invalid token"}}`, http.StatusUnauthorized)
	})
	e := &Engine{Provider: p}
	in := baseInput()
	res, err := e.Evaluate(context.Background(), in)
	if err == nil || res != nil {
		t.Fatalf("auth failure must fail, never fall back to demo")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("error should mention status: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("auth failure must not burn a retry (calls=%d)", got)
	}
}

func TestDeepInfraRateLimitNoRetry(t *testing.T) {
	var calls int32
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":{"message":"quota"}}`, http.StatusTooManyRequests)
	})
	e := &Engine{Provider: p}
	_, err := e.Evaluate(context.Background(), baseInput())
	if err == nil {
		t.Fatal("rate limit must surface as failure")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("rate limit must not retry (calls=%d)", got)
	}
}

func TestDeepInfraMalformedThenValidSingleRetry(t *testing.T) {
	var calls int32
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			// DeepInfra-ish edge: markdown-fenced valid JSON is stripped, so
			// force genuinely malformed content on the first call.
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not json at all"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quoteJSON(fullEvalJSON()) + `}}]}`))
	})
	e := &Engine{Provider: p}
	res, err := e.Evaluate(context.Background(), baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected exactly one retry for malformed output, calls=%d", got)
	}
	if res.TotalScore != 65 {
		t.Fatalf("parsed score after retry: %.2f", res.TotalScore)
	}
}

func TestDeepInfraMalformedTwiceBudgetExhausted(t *testing.T) {
	var calls int32
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"still not json"}}]}`))
	})
	e := &Engine{Provider: p}
	_, err := e.Evaluate(context.Background(), baseInput())
	if err == nil || !strings.Contains(err.Error(), "2 attempts") {
		t.Fatalf("expected budget exhaustion error, got %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("budget must cap provider calls at 2, got %d", got)
	}
}

func TestDeepInfraTransientServerErrorThenValid(t *testing.T) {
	var calls int32
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + quoteJSON(fullEvalJSON()) + `}}]}`))
	})
	e := &Engine{Provider: p}
	res, err := e.Evaluate(context.Background(), baseInput())
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalScore != 65 {
		t.Fatalf("recovered result: %.2f", res.TotalScore)
	}
}

func TestProbeOneTinyRequest(t *testing.T) {
	var calls int32
	var maxTok int
	p := fakeProvider(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		maxTok = req.MaxTokens
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	})
	res, err := Probe(context.Background(), p, 32)
	if err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("probe must make exactly one request, got %d", got)
	}
	if maxTok != 32 {
		t.Fatalf("probe token budget wrong: %d", maxTok)
	}
	if !strings.Contains(res.Raw, "ok") {
		t.Fatalf("probe reply lost: %s", res.Raw)
	}
}

func TestMaskNeverExposesKey(t *testing.T) {
	m := SafeMask("deepinfra-sk-1234567890abcd")
	if strings.Contains(m, "1234567890") || len(m) > 8 {
		t.Fatalf("mask leaks too much: %q", m)
	}
	if SafeMask("") != "(not configured)" {
		t.Errorf("empty mask wrong")
	}
}
