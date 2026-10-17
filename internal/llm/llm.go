// Package llm is a minimal client for OpenAI-compatible Chat Completions, used
// with Groq (https://api.groq.com/openai/v1). Parameters follow Groq's docs
// (2026-10-17): gpt-oss models support tool use and reasoning_effort, do not
// support parallel tool calls, and return reasoning in message.reasoning when
// reasoning_format is "parsed" (the default with tools).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	BaseURL string
	APIKey  string
}

func ConfigFromEnv() (Config, error) {
	c := Config{BaseURL: os.Getenv("GROQ_BASE_URL"), APIKey: os.Getenv("GROQ_API_KEY")}
	if c.BaseURL == "" || c.APIKey == "" {
		return c, errors.New("llm: GROQ_BASE_URL and GROQ_API_KEY are required")
	}
	return c, nil
}

type Client struct {
	cfg         Config
	http        *http.Client
	maxAttempts int
	sleep       func(context.Context, time.Duration) error
}

func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg, http: &http.Client{Timeout: 60 * time.Second}, maxAttempts: 4, sleep: sleepCtx}
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Reasoning  string     `json:"reasoning,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// wireMessage omits reasoning when sending history back to the API.
type wireMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Request struct {
	Model               string
	Messages            []Message
	Tools               []ToolSpec
	ReasoningEffort     string // "low" | "medium" | "high" (gpt-oss)
	Temperature         float64
	MaxCompletionTokens int
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Response struct {
	Message      Message
	FinishReason string
	Usage        Usage
}

type APIError struct {
	Status  int
	Type    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("llm: %d %s: %s", e.Status, e.Type, e.Message)
}

func (c *Client) Complete(ctx context.Context, r Request) (Response, error) {
	type tool struct {
		Type     string   `json:"type"`
		Function ToolSpec `json:"function"`
	}
	msgs := make([]wireMessage, len(r.Messages))
	for i, m := range r.Messages {
		msgs[i] = wireMessage{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}
	}
	body := map[string]any{"model": r.Model, "messages": msgs}
	if len(r.Tools) > 0 {
		ts := make([]tool, len(r.Tools))
		for i, t := range r.Tools {
			ts[i] = tool{Type: "function", Function: t}
		}
		body["tools"] = ts
		body["tool_choice"] = "auto"
	}
	if r.ReasoningEffort != "" {
		body["reasoning_effort"] = r.ReasoningEffort
	}
	if r.Temperature > 0 {
		body["temperature"] = r.Temperature
	}
	if r.MaxCompletionTokens > 0 {
		body["max_completion_tokens"] = r.MaxCompletionTokens
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, err
	}
	var lastErr error
	for attempt := 0; attempt < c.maxAttempts; attempt++ {
		resp, wait, err := c.once(ctx, payload)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if wait < 0 {
			return Response{}, err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return Response{}, err
		}
	}
	return Response{}, lastErr
}

// once returns wait < 0 when the error is not retryable.
func (c *Client) once(ctx context.Context, payload []byte) (Response, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, -1, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, -1, err
		}
		return Response{}, time.Second, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Response{}, time.Second, err
	}
	if resp.StatusCode/100 != 2 {
		var env struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		apiErr := &APIError{Status: resp.StatusCode, Type: env.Error.Type, Message: env.Error.Message}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			// Groq sets retry-after (seconds) only on 429.
			wait := 2 * time.Second
			if s, err := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); err == nil && s >= 0 {
				wait = time.Duration(s * float64(time.Second))
			}
			return Response{}, min(wait, 30*time.Second), apiErr
		case resp.StatusCode >= 500:
			return Response{}, 2 * time.Second, apiErr
		}
		return Response{}, -1, apiErr
	}
	var out struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, -1, err
	}
	if len(out.Choices) == 0 {
		return Response{}, -1, errors.New("llm: empty choices")
	}
	return Response{Message: out.Choices[0].Message, FinishReason: out.Choices[0].FinishReason, Usage: out.Usage}, 0, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
