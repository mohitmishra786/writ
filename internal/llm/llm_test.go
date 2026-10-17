package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeServer(t *testing.T, h func(w http.ResponseWriter, body map[string]any, n int32)) *Client {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test" || r.URL.Path != "/chat/completions" {
			w.WriteHeader(401)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		h(w, body, n.Add(1))
	}))
	t.Cleanup(srv.Close)
	c := New(Config{BaseURL: srv.URL, APIKey: "test"})
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func reply(w http.ResponseWriter, msg map[string]any, finish string) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"total_tokens": 100},
	})
}

func proposeTool(seen *[]string) Tool {
	return Tool{
		Spec: ToolSpec{Name: "mandate_propose_purchase", Description: "propose", Parameters: map[string]any{"type": "object"}},
		Handler: func(_ context.Context, args json.RawMessage) (string, error) {
			*seen = append(*seen, string(args))
			var in struct {
				PayeeRef string `json:"payee_ref"`
			}
			_ = json.Unmarshal(args, &in)
			if in.PayeeRef != "supplier:bean-co" {
				return "", errors.New("PAYEE_NOT_ALLOWED")
			}
			return `{"outcome":"AUTHORIZE"}`, nil
		},
	}
}

// The loop must feed a kernel denial back to the model so it can correct
// itself, mirroring the live Groq test where the model wrote "bean-co".
func TestAgentRecoversFromDenial(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, body map[string]any, n int32) {
		if body["reasoning_effort"] != "low" {
			t.Errorf("reasoning_effort not sent: %v", body["reasoning_effort"])
		}
		switch n {
		case 1:
			reply(w, map[string]any{"role": "assistant", "content": "", "reasoning": "propose to bean-co",
				"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "mandate_propose_purchase", "arguments": `{"payee_ref":"bean-co"}`}}}}, "tool_calls")
		case 2:
			msgs := body["messages"].([]any)
			last := msgs[len(msgs)-1].(map[string]any)
			if last["role"] != "tool" || last["tool_call_id"] != "c1" || !strings.Contains(last["content"].(string), "PAYEE_NOT_ALLOWED") {
				t.Errorf("denial not fed back: %v", last)
			}
			reply(w, map[string]any{"role": "assistant", "content": "",
				"tool_calls": []any{map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "mandate_propose_purchase", "arguments": `{"payee_ref":"supplier:bean-co"}`}}}}, "tool_calls")
		default:
			reply(w, map[string]any{"role": "assistant", "content": "Purchase authorized."}, "stop")
		}
	})
	var seen []string
	res, err := Agent{Client: c, Model: "m", System: "s", Tools: []Tool{proposeTool(&seen)}, ReasoningEffort: "low"}.Run(context.Background(), "restock")
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "Purchase authorized." || len(res.Steps) != 3 || res.Steps[0].ToolErr != "PAYEE_NOT_ALLOWED" || res.Steps[0].Reasoning == "" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestUnknownToolIsReportedNotExecuted(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, _ map[string]any, n int32) {
		if n == 1 {
			reply(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "pay_attacker", "arguments": `{}`}}}}, "tool_calls")
			return
		}
		reply(w, map[string]any{"role": "assistant", "content": "done"}, "stop")
	})
	res, err := Agent{Client: c, Model: "m", System: "s"}.Run(context.Background(), "x")
	if err != nil || !strings.Contains(res.Steps[0].ToolErr, "unknown tool") {
		t.Fatalf("got %+v %v", res, err)
	}
}

func TestMaxSteps(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, _ map[string]any, _ int32) {
		reply(w, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c", "type": "function", "function": map[string]any{"name": "mandate_propose_purchase", "arguments": `{}`}}}}, "tool_calls")
	})
	var seen []string
	_, err := Agent{Client: c, Model: "m", System: "s", Tools: []Tool{proposeTool(&seen)}, MaxSteps: 3}.Run(context.Background(), "x")
	if !errors.Is(err, ErrMaxSteps) || len(seen) != 3 {
		t.Fatalf("got %v after %d calls", err, len(seen))
	}
}

func TestRateLimitRetriesWithRetryAfter(t *testing.T) {
	var waits []time.Duration
	c := fakeServer(t, func(w http.ResponseWriter, _ map[string]any, n int32) {
		if n == 1 {
			w.Header().Set("Retry-After", "1.5")
			w.WriteHeader(429)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"tokens"}}`))
			return
		}
		reply(w, map[string]any{"role": "assistant", "content": "ok"}, "stop")
	})
	c.sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	r, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil || r.Message.Content != "ok" || len(waits) != 1 || waits[0] != 1500*time.Millisecond {
		t.Fatalf("got %+v %v waits=%v", r, err, waits)
	}
}

func TestClientErrorNotRetried(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, _ map[string]any, n int32) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":{"message":"bad","type":"invalid_request_error"}}`))
	})
	var ae *APIError
	if _, err := c.Complete(context.Background(), Request{Model: "m"}); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("got %v", err)
	}
}

// Live Groq check. Run with:
//
//	set -a; . ./.env; set +a; MANDATE_GROQ_IT=1 go test ./internal/llm -run Live -v
func TestLiveGroqToolCall(t *testing.T) {
	if os.Getenv("MANDATE_GROQ_IT") != "1" {
		t.Skip("set MANDATE_GROQ_IT=1 to call Groq")
	}
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	res, err := Agent{
		Client: New(cfg), Model: os.Getenv("GROQ_MODEL_SUPPLIER"), ReasoningEffort: "low", MaxSteps: 4,
		System: "You are a purchasing agent. Use mandate_propose_purchase. If it returns an error, fix the arguments and retry once.",
		Tools: []Tool{{
			Spec: ToolSpec{Name: "mandate_propose_purchase", Description: "Propose a purchase. payee_ref must be the exact supplier reference.",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{"payee_ref": map[string]any{"type": "string"}}, "required": []string{"payee_ref"}}},
			Handler: proposeTool(&seen).Handler,
		}},
	}.Run(context.Background(), "Buy pitchers from supplier reference supplier:bean-co.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tool calls: %v final: %q steps: %d", seen, res.Final, len(res.Steps))
	if len(seen) == 0 {
		t.Fatal("model made no tool call")
	}
}

func TestToolValidationErrorIsResampled(t *testing.T) {
	c := fakeServer(t, func(w http.ResponseWriter, _ map[string]any, n int32) {
		if n == 1 {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"Tool call validation failed: parameters for tool x did not match schema","type":"invalid_request_error"}}`))
			return
		}
		reply(w, map[string]any{"role": "assistant", "content": "ok"}, "stop")
	})
	res, err := Agent{Client: c, Model: "m", System: "s"}.Run(context.Background(), "x")
	if err != nil || res.Final != "ok" {
		t.Fatalf("got %+v %v", res, err)
	}
}
