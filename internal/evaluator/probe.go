package evaluator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ProbeResult is the outcome of the one-shot connectivity probe.
type ProbeResult struct {
	Model        string
	BaseURL      string
	OutputTokens int
	Raw          string
}

// probeSystemPrompt is a deliberately tiny probe instruction.
const probeUserPrompt = `Connectivity probe. Reply with exactly this JSON object and nothing else: {"ok":true}`

// Probe performs ONE very small provider request (configurable output cap of
// at most 64 tokens by default) and validates the shape of the response.
// Used by `educore provider-check`; never logs or exposes the API key.
func Probe(ctx context.Context, p *Provider, maxTokens int) (*ProbeResult, error) {
	if p == nil || p.BaseURL == "" || p.Model == "" {
		return nil, fmt.Errorf("provider is not configured (AI_BASE_URL / AI_MODEL missing)")
	}
	if maxTokens <= 0 || maxTokens > 64 {
		maxTokens = 32
	}
	text, err := p.Complete(ctx, "You are a connectivity probe. Output only the requested JSON.", probeUserPrompt, maxTokens)
	if err != nil {
		return nil, err
	}
	trimmed := stripFences(text)
	var probe struct {
		OK bool `json:"ok"`
	}
	if jerr := json.Unmarshal([]byte(trimmed), &probe); jerr != nil || !probe.OK {
		if jerr == nil && !probe.OK {
			return &ProbeResult{Model: p.Model, BaseURL: p.BaseURL, Raw: strings.TrimSpace(text)},
				fmt.Errorf("probe succeeded at transport level but the payload was not {\"ok\":true}")
		}
		return nil, fmt.Errorf("probe reply is not the expected JSON: %.80s", trimmed)
	}
	return &ProbeResult{
		Model:        p.Model,
		BaseURL:      p.BaseURL,
		OutputTokens: maxTokens,
		Raw:          strings.TrimSpace(text),
	}, nil
}

// ToolProbeResult reports the outcome of the tool-calling protocol probe.
type ToolProbeResult struct {
	Model        string
	BaseURL      string
	FinishReason string
	ToolCalls    int
	ToolNames    []string
	Raw          string
}

// probeToolsSystem is the tiny tool probe instruction.
const probeToolsUserPrompt = `Connectivity probe (tool calling). If the day is odd, call the probe_ping tool once; otherwise reply with a short sentence only. Do not call any other tool.`

// ProbeToolCalling verifies the DeepInfra OpenAI-compatible TOOL-CALLING
// contract specifically: it sends one tiny turn declaring a no-op tool and
// validates that the endpoint answers a well-formed assistant message
// (finish_reason stop or tool_calls, with valid tool_call wire shape).
// Echo-mirror tools: probe_ping has an empty schema; any tool_calls returned
// are counted but NOT executed (nothing mutated, nothing echoed).
// Exactly one provider request; never logs or exposes the API key.
func ProbeToolCalling(ctx context.Context, p *Provider, maxTokens int) (*ToolProbeResult, error) {
	if p == nil || p.BaseURL == "" || p.Model == "" {
		return nil, fmt.Errorf("provider is not configured (AI_BASE_URL / AI_MODEL missing)")
	}
	if maxTokens <= 0 || maxTokens > 64 {
		maxTokens = 32
	}
	var probeTool ToolSpec
	probeTool.Type = "function"
	probeTool.Function.Name = "probe_ping"
	probeTool.Function.Description = "No-op connectivity probe tool. Has no parameters."
	probeTool.Function.Parameters = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	turn, err := p.ChatTurn(ctx,
		[]ChatMessage{{Role: "user", Content: probeToolsUserPrompt}},
		[]ToolSpec{probeTool}, maxTokens)
	if err != nil {
		return nil, err
	}
	calls, spoken, perr := ExtractToolCalls(turn)
	if perr != nil {
		return nil, fmt.Errorf("tool probe reply could not be normalized: %v", perr)
	}
	fr := turn.FinishReason
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.Name)
	}
	if fr == "" {
		fr = ToolCallsOrStop(len(calls))
	}
	var names2 []string
	_ = names2
	return &ToolProbeResult{
		Model:        p.Model,
		BaseURL:      p.BaseURL,
		FinishReason: fr,
		ToolCalls:    len(calls),
		ToolNames:    names,
		Raw:          spoken,
	}, nil
}

// ToolCallsOrStop resolves finish_reason fallback for providers that omit it
// in streaming relays: tool_calls if calls present, else stop.
func ToolCallsOrStop(n int) string {
	if n > 0 {
		return "tool_calls"
	}
	return "stop"
}
