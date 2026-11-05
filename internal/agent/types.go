// Package agent implements the bounded, tool-using evaluation agent: a typed
// tool registry over the store, the model-driven execution loop with dynamic
// tool selection via OpenAI-compatible tool calling, persistent execution
// state and a full trace. Existing business logic (store queries, evaluation
// result validation, persistence) is reused — the agent never duplicates it.
package agent

import "encoding/json"

// Run states.
const (
	StatusPending = "PENDING"
	StatusRunning = "RUNNING"
	StatusDone    = "DONE"
	StatusFailed  = "FAILED"
)

// Run kinds.
const (
	KindEvaluate   = "EVALUATE"
	KindReconsider = "RECONSIDER"
	KindFollowup   = "FOLLOWUP"
)

// Trace event kinds (mirrors agent_events CHECK).
const (
	EventModelCall  = "MODEL_CALL"
	EventToolCall   = "TOOL_CALL"
	EventToolResult = "TOOL_RESULT"
	EventToolError  = "TOOL_ERROR"
	EventFinal      = "FINAL"
	EventError      = "ERROR"
	EventUser       = "USER_MSG"
	EventNote       = "NOTE"
)

// DefaultCaps are the execution bounds.
const (
	DefaultMaxIterations  = 12
	DefaultMaxToolCalls   = 24
	DefaultMaxOutputChars = 200_000 // accumulated model output budget
)

// ToolRef is a model tool invocation.
type ToolRef struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatMessage is one conversation turn. Assistant messages may carry raw
// tool_calls; tool observations use Role "tool" with ToolCallID.
type ChatMessage struct {
	Role       string          `json:"role"` // system|user|assistant|tool
	Content    string          `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty,omitempty"` // raw passthrough
	ToolCallID string          `json:"-"`
}

// MarshalJSON for assistant tool messages mirrors the OpenAI wire format.
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role       string          `json:"role"`
		Content    string          `json:"content"`
		ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallID string          `json:"tool_call_id,omitempty"`
	}
	w := wire{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}
	return json.Marshal(w)
}
