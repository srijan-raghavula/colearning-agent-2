package evaluator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeToolCallingToolCallContract(t *testing.T) {
	// Provider answers with a tool_call — the wire shape must parse.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_9","type":"function","function":{"name":"probe_ping","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()
	p := &Provider{BaseURL: srv.URL, APIKey: "sk-test", Model: "m1"}
	res, err := ProbeToolCalling(context.Background(), p, 32)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != "tool_calls" || res.ToolCalls != 1 || res.ToolNames[0] != "probe_ping" {
		t.Fatalf("tool probe parse: %+v", res)
	}
}

func TestProbeToolCallingStopContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"no tool needed."},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()
	p := &Provider{BaseURL: srv.URL, APIKey: "sk-test", Model: "m1"}
	res, err := ProbeToolCalling(context.Background(), p, 32)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != "stop" || res.ToolCalls != 0 || res.Raw != "no tool needed." {
		t.Fatalf("tool probe (stop) parse: %+v", res)
	}
}

func TestProbeToolCallingUnparsableToolCallFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":"not-an-array"},"finish_reason":"tool_calls"}]}`))
	}))
	defer srv.Close()
	p := &Provider{BaseURL: srv.URL, APIKey: "sk-test", Model: "m1"}
	if _, err := ProbeToolCalling(context.Background(), p, 32); err == nil {
		t.Fatal("unparsable tool_calls must fail the probe")
	}
}
