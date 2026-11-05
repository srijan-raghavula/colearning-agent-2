package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"educore/internal/evaluator"
)

// Proves the CLI provider-check flow against a fake DeepInfra-shaped server:
// correct path, Bearer key in the header, JSON probe reply parsed. No real
// network is touched and no credits are consumed.
func TestProviderCheckFlowWithFakeServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/openai/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer probe-key-987654321" {
			t.Errorf("auth header missing/incorrect")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))
	}))
	defer srv.Close()
	p := &evaluator.Provider{BaseURL: srv.URL + "/v1/openai", APIKey: "probe-key-987654321", Model: "test/model"}
	res, err := evaluator.Probe(context.Background(), p, 32)
	if err != nil {
		t.Fatal(err)
	}
	if res.Raw != `{"ok":true}` {
		t.Fatalf("probe reply: %q", res.Raw)
	}
	if evaluator.SafeMask("probe-key-987654321") == "probe-key-987654321" {
		t.Fatal("mask failed")
	}
}
