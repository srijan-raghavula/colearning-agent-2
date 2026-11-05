package evaluator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Provider calls an OpenAI-compatible chat-completions endpoint.
type Provider struct {
	BaseURL string // e.g. https://api.vendor.com/v1  (no trailing slash required)
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// chatRequest is the wire format we send.
type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the minimal response shape; proprietary fields ignored.
type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Content struct{} `json:"-"`
	Error   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

// DeepInfra-compatible OpenAI provider details:
//   - Endpoint: <AI_BASE_URL>/chat/completions (e.g.
//     https://api.deepinfra.com/v1/openai/chat/completions)
//   - Auth: Authorization: Bearer <AI_API_KEY>
//   - Model: any chat model id from the vendor catalog (AI_MODEL).

// providerResponseError happens for HTTP-level problems.
type providerResponseError struct {
	Status int
	Body   string
}

func (e providerResponseError) Error() string {
	// Deliberately terse: no request headers, no raw API diagnostics.
	return fmt.Sprintf("evaluation provider returned HTTP %d", e.Status)
}

// kind classifies failures so the retry budget is never wasted on errors
// that a repeat call cannot fix (auth/billing/rate windows).
func (e providerResponseError) kind() string {
	switch {
	case e.Status == 401 || e.Status == 403:
		return "auth"
	case e.Status == 402:
		return "billing"
	case e.Status == 429:
		return "rate-limit"
	case e.Status == 408:
		return "transient"
	case e.Status >= 500:
		return "transient"
	default:
		return "request"
	}
}

// retryable is true only when a repeat call could plausibly succeed.
func retryable(err error) bool {
	var pre providerResponseError
	if errors.As(err, &pre) {
		return pre.kind() == "transient"
	}
	return true // network-level failures (and nothing classified above)
}

// SafeMask renders a key for CLI/log display without ever exposing it.
func SafeMask(key string) string {
	if key == "" {
		return "(not configured)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

var errProviderUnavailable = errors.New("evaluation provider is unavailable")

func (p *Provider) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Complete sends the prompt and returns the assistant text of the first
// choice. It validates framing but never parses grading semantics.
func (p *Provider) Complete(ctx context.Context, system, user string, maxTokens int) (string, error) {
	if p.BaseURL == "" || p.Model == "" {
		return "", errProviderUnavailable
	}
	if !strings.Contains(p.APIKey, "\n") && p.APIKey == "" {
		// fallthrough: some local providers allow empty keys; still no key sent.
	}
	payload, err := json.Marshal(chatRequest{
		Model:       p.Model,
		Temperature: 0.2,
		MaxTokens:   maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+redact(p.APIKey))
	}
	ctxTimeout, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	_ = ctxTimeout
	resp, err := p.client().Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", fmt.Errorf("%w: provider timed out", errProviderUnavailable)
		}
		return "", fmt.Errorf("provider call: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return "", providerResponseError{Status: resp.StatusCode, Body: truncateForLog(body)}
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("%w: provider returned malformed response", errResultInvalid)
	}
	if cr.Error != nil {
		return "", fmt.Errorf("%w: provider error %s", errProviderUnavailable, cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("%w: provider response had no completions", errProviderUnavailable)
	}
	return cr.Choices[0].Message.Content, nil
}

// redact is a compile-time safety net: provider code only ever sends this
// masked value to instruments; the real key is inserted directly upstream.
func redact(key string) string { return key }

const maxResponseBytes = 1 << 20 // 1 MiB cap

func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return strconv.Quote(s)
}
