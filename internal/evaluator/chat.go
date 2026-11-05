package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatMessage is one conversation turn in OpenAI wire format:
// content is a plain string (or empty), tool_calls is a separate optional
// array, and tool_call_id is set on "tool" observation messages that answer
// a specific assistant tool_call. Assistant tool turns carry their tool_calls
// verbatim so they can be replayed unchanged in subsequent turns.
type ChatMessage struct {
	Role         string          `json:"role"`
	Content      string          `json:"content"`
	ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	FinishReason string          `json:"finish_reason,omitempty"`
}

// ToolCall is one normalized assistant tool invocation.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MarshalJSON emits the exact OpenAI chat-completions message shape:
// {"role":..,"content":..,"tool_calls":[..],"tool_call_id":..}
func (m ChatMessage) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role         string          `json:"role"`
		Content      string          `json:"content"`
		ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallID   string          `json:"tool_call_id,omitempty"`
		FinishReason string          `json:"finish_reason,omitempty"`
	}
	w := wire{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, FinishReason: m.FinishReason}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ToolSpec declares one callable function (OpenAI tool schema).
type ToolSpec struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type chatToolsRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolSpec    `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// chatToolsResponse parses the tool-calling conversation response.
type chatToolsResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ChatTurn sends one full conversation turn (with tool definitions) and
// returns the assistant message in the OpenAI wire shape. The returned
// ChatMessage.Content is the plain text content ("" when only tool calls are
// requested) and ToolCalls holds the raw tool_calls array when the model
// requested tool execution. FinishReason is propagated so the caller can react
// to budget truncation (e.g. "length").
func (p *Provider) ChatTurn(ctx context.Context, msgs []ChatMessage, tools []ToolSpec, maxTokens int) (ChatMessage, error) {
	if p.BaseURL == "" || p.Model == "" {
		return ChatMessage{}, errProviderUnavailable
	}
	payload, err := json.Marshal(chatToolsRequest{
		Model:       p.Model,
		Temperature: 0.2,
		Messages:    msgs,
		Tools:       tools,
		MaxTokens:   maxTokens,
	})
	if err != nil {
		return ChatMessage{}, fmt.Errorf("build request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(p.BaseURL, "/")+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatMessage{}, fmt.Errorf("provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 180 * time.Second} // agent turns can be longer
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ChatMessage{}, fmt.Errorf("agent turn timed out")
		}
		return ChatMessage{}, fmt.Errorf("provider call: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes*4))
	if resp.StatusCode != http.StatusOK {
		return ChatMessage{}, providerResponseError{Status: resp.StatusCode, Body: truncateForLog(body)}
	}
	var out chatToolsResponse
	if jerr := json.Unmarshal(body, &out); jerr != nil {
		return ChatMessage{}, fmt.Errorf("%w: malformed provider conversation response", errResultInvalid)
	}
	if out.Error != nil {
		return ChatMessage{}, fmt.Errorf("%w: provider error: %s", errProviderUnavailable, out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return ChatMessage{}, fmt.Errorf("%w: conversation response had no choices", errProviderUnavailable)
	}
	msg := out.Choices[0].Message
	toolCallsRaw := json.RawMessage(nil)
	if len(msg.ToolCalls) > 0 {
		var tcs []ToolCall
		for _, tc := range msg.ToolCalls {
			tcs = append(tcs, ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		tcB, terr := json.Marshal(tcs)
		if terr != nil {
			return ChatMessage{}, fmt.Errorf("%w: marshal tool_calls: %v", errResultInvalid, terr)
		}
		// Re-emit in the canonical OpenAI tool_calls shape.
		type tcWire struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		var wireTcs []tcWire
		for _, t := range tcs {
			var fw tcWire
			fw.ID = t.ID
			fw.Type = "function"
			fw.Function.Name = t.Name
			fw.Function.Arguments = t.Arguments
			wireTcs = append(wireTcs, fw)
		}
		tcB, _ = json.Marshal(wireTcs)
		toolCallsRaw = tcB
	}
	return ChatMessage{
		Role:         msg.Role,
		Content:      msg.Content,
		ToolCalls:    toolCallsRaw,
		FinishReason: out.Choices[0].FinishReason,
	}, nil
}

// ToolRef is a normalized tool invocation extracted from an assistant turn.
type ToolRef struct {
	ID        string
	Name      string
	Arguments string
}

// ExtractToolCalls normalizes the tool_calls array of a ChatMessage into
// ToolRefs. content is returned as the spoken text. Errors when the assistant
// message cannot be normalized.
func ExtractToolCalls(m ChatMessage) ([]ToolRef, string, error) {
	if m.ToolCalls == nil {
		// No tool calls requested; return empty calls and the spoken content.
		return nil, m.Content, nil
	}
	var tcs []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(m.ToolCalls, &tcs); err != nil {
		return nil, "", fmt.Errorf("%w: assistant tool_calls unparsable: %v", errResultInvalid, err)
	}
	var calls []ToolRef
	for _, tc := range tcs {
		calls = append(calls, ToolRef{ID: tc.ID, Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return calls, m.Content, nil
}
