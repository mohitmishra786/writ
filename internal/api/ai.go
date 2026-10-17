package api

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
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// The Studio AI panel speaks the OpenAI Responses API (web/console/src/ai).
// Turns are served by an ordered chain of provider routes:
//
//  1. NVIDIA API catalog (integrate.api.nvidia.com). It exposes OpenAI chat
//     completions only (/v1/responses is 404 there), so requests and
//     responses are translated here.
//  2. Groq free tier, which implements the Responses API natively
//     (https://console.groq.com/docs/responses-api) but allows 8K tokens per
//     minute per model, less than two Studio turns.
//
//  0. Claude (native Messages API) when ANTHROPIC_API_KEY is set; Gemini
//     before NVIDIA when GEMINI_API_KEY is set.
//
// A route that is rate limited, times out, or fails moves the turn to the
// next route. Keys stay on the server.

type aiRoute struct {
	provider string
	base     string
	key      string
	model    string
	chat     bool // OpenAI chat completions instead of Responses
	gemini   bool // carries Gemini thought signatures on tool calls
	claude   bool // native Anthropic Messages API
	maxOut   int
	timeout  time.Duration
}

func envList(name, def string) []string {
	v := os.Getenv(name)
	if v == "" {
		v = def
	}
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func aiRoutes() []aiRoute {
	var routes []aiRoute
	// Claude Haiku first when a key is configured (see ai_anthropic.go).
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		for _, m := range envList("STUDIO_AI_ANTHROPIC_MODELS", "claude-haiku-5-5") {
			routes = append(routes, aiRoute{provider: "anthropic", base: "https://api.anthropic.com/v1", key: key,
				model: m, claude: true, maxOut: 8192, timeout: 60 * time.Second})
		}
	}
	// Gemini through its OpenAI-compatible endpoint
	// (https://ai.google.dev/gemini-api/docs/openai). On the free tier Flash
	// models allow 5 requests per minute and 20 per day per model, so a busy
	// demo falls through to the next provider quickly (429, no wait).
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		for _, m := range envList("STUDIO_AI_GEMINI_MODELS", "gemini-3.8-flash") {
			routes = append(routes, aiRoute{provider: "gemini", base: "https://generativelanguage.googleapis.com/v1beta/openai", key: key,
				model: m, chat: true, gemini: true, maxOut: 8192, timeout: 45 * time.Second})
		}
	}
	if key := os.Getenv("NVIDIA_API_KEY"); key != "" {
		base := strings.TrimRight(os.Getenv("NVIDIA_BASE_URL"), "/")
		if base == "" {
			base = "https://integrate.api.nvidia.com/v1"
		}
		// Order from a replay of captured Studio turns (schema-valid tool
		// calls, latency): step-5-preview 14/14 valid at ~5s median,
		// nemotron-3-super 9/14 at ~3-9s, glm-5.3-flash 9/14 at ~26-42s.
		// Hosted NVIDIA endpoints can queue for minutes under load, so each
		// model gets a bounded wait before the turn falls through.
		for _, m := range envList("STUDIO_AI_NVIDIA_MODELS", "stepfun-ai/step-5-preview,nvidia/nemotron-3-super-120b-a12b,z-ai/glm-5.3-flash") {
			routes = append(routes, aiRoute{provider: "nvidia", base: base, key: key, model: m, chat: true, maxOut: 8192, timeout: 45 * time.Second})
		}
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		base := strings.TrimRight(os.Getenv("GROQ_BASE_URL"), "/")
		// Groq counts max_output_tokens against the per-minute budget when a
		// request is admitted, so keep the cap small on the free plan.
		maxOut := 2048
		if v, err := strconv.Atoi(os.Getenv("STUDIO_AI_MAX_OUTPUT_TOKENS")); err == nil && v > 0 {
			maxOut = v
		}
		for _, m := range envList("STUDIO_AI_GROQ_MODELS", "openai/gpt-oss-120b,openai/gpt-oss-20b") {
			routes = append(routes, aiRoute{provider: "groq", base: base, key: key, model: m, maxOut: maxOut, timeout: 120 * time.Second})
		}
	}
	return routes
}

type aiResult struct {
	status int
	body   []byte
	retry  string
	cache  cacheUsage
}

// aiResponses serves one Studio agent turn (POST /v1/ai/responses).
func (s *Server) aiResponses(w http.ResponseWriter, r *http.Request) {
	routes := aiRoutes()
	if len(routes) == 0 {
		writeErr(w, http.StatusServiceUnavailable, "AI is not configured")
		return
	}
	var in map[string]any
	if !decodeLoose(w, r, &in) {
		return
	}
	// Forward only the fields the Studio adapter sends; providers reject the
	// OpenAI-only ones they do not support (store, previous_response_id, ...).
	req := map[string]any{"input": in["input"]}
	for _, k := range []string{"instructions", "tools", "tool_choice", "text", "temperature"} {
		if v, ok := in[k]; ok && v != nil {
			req[k] = v
		}
	}

	// Development aid: STUDIO_AI_CAPTURE names a file that receives each
	// translated-ready request as one JSON line, for offline model evaluation.
	if path := os.Getenv("STUDIO_AI_CAPTURE"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			b, _ := json.Marshal(req)
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
	var res aiResult
	turnID := strings.ToLower(ulid.Make().String())
	for i, rt := range routes {
		var err error
		start := time.Now()
		res, err = s.aiTry(r.Context(), rt, req)
		if r.Context().Err() != nil {
			return
		}
		s.recordAICall(turnID, i, rt, res, err, time.Since(start))
		if err == nil && res.status < 300 {
			break
		}
		msg := ""
		if err != nil {
			msg = err.Error()
		} else {
			msg = string(res.body[:min(len(res.body), 300)])
		}
		s.log.Warn("ai route failed, trying next", "provider", rt.provider, "model", rt.model, "status", res.status, "err", msg)
		if err != nil {
			res = aiResult{status: http.StatusBadGateway, body: errorBody(rt.provider + " unavailable: " + msg)}
		}
	}
	if res.status >= 300 {
		res.body = normalizeError(res.status, res.body)
	}
	w.Header().Set("Content-Type", "application/json")
	if res.retry != "" {
		w.Header().Set("Retry-After", res.retry)
	}
	w.WriteHeader(res.status)
	_, _ = w.Write(res.body)
}

// aiTry runs one route. Groq validates tool-call arguments server-side and
// answers 400 "Tool call validation failed" when the model samples arguments
// that do not match the schema; that is a sampling error, so it is resampled
// a bounded number of times (as internal/llm does for the procurement agent).
// A short Groq retry-after is waited out once.
func (s *Server) aiTry(ctx context.Context, rt aiRoute, req map[string]any) (aiResult, error) {
	resamples, waited := 0, false
	for {
		res, err := s.aiCall(ctx, rt, req)
		if err != nil {
			return res, err
		}
		if res.status == http.StatusBadRequest && resamples < 2 && strings.Contains(strings.ToLower(string(res.body)), "tool call validation failed") {
			resamples++
			continue
		}
		if res.status == http.StatusTooManyRequests && !waited {
			if wait, perr := strconv.ParseFloat(res.retry, 64); perr == nil && wait <= 8 {
				waited = true
				select {
				case <-time.After(time.Duration(wait*1000+250) * time.Millisecond):
					continue
				case <-ctx.Done():
					return res, ctx.Err()
				}
			}
		}
		return res, nil
	}
}

func (s *Server) aiCall(ctx context.Context, rt aiRoute, req map[string]any) (aiResult, error) {
	if rt.claude {
		res, err := s.claudeCall(ctx, rt, req, true)
		if err == nil && isStrictSchemaError(res.status, res.body) {
			s.log.Warn("claude rejected a strict tool schema; retrying without strict", "body", string(res.body[:min(len(res.body), 300)]))
			res, err = s.claudeCall(ctx, rt, req, false)
		}
		return res, err
	}
	var (
		payload any
		path    string
	)
	if rt.chat {
		chat, err := responsesToChat(req, rt.gemini)
		if err != nil {
			return aiResult{}, err
		}
		chat["model"], chat["max_tokens"] = rt.model, rt.maxOut
		payload, path = chat, "/chat/completions"
	} else {
		body := make(map[string]any, len(req)+3)
		for k, v := range req {
			body[k] = v
		}
		body["model"], body["max_output_tokens"], body["stream"] = rt.model, rt.maxOut, false
		payload, path = body, "/responses"
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return aiResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, rt.timeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, rt.base+path, bytes.NewReader(b))
	if err != nil {
		return aiResult{}, err
	}
	hreq.Header.Set("Authorization", "Bearer "+rt.key)
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return aiResult{}, fmt.Errorf("no response within %s", rt.timeout)
		}
		return aiResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return aiResult{}, err
	}
	res := aiResult{status: resp.StatusCode, body: out, retry: resp.Header.Get("Retry-After")}
	if rt.chat && resp.StatusCode < 300 {
		conv, err := chatToResponses(out, rt.model)
		if rt.gemini {
			rememberSignatures(out)
		}
		if err != nil {
			return aiResult{}, err
		}
		res.body = conv
	}
	return res, nil
}

// responsesToChat translates a Responses API request into a chat completions
// request. Consecutive function_call items become one assistant message with
// tool_calls; function_call_output items become tool messages.
func responsesToChat(req map[string]any, gemini bool) (map[string]any, error) {
	var msgs []map[string]any
	if ins, _ := req["instructions"].(string); ins != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": ins})
	}
	switch input := req["input"].(type) {
	case string:
		msgs = append(msgs, map[string]any{"role": "user", "content": input})
	case []any:
		var pending []map[string]any // tool calls awaiting their assistant message
		flush := func() {
			if len(pending) > 0 {
				msgs = append(msgs, map[string]any{"role": "assistant", "content": "", "tool_calls": pending})
				pending = nil
			}
		}
		for _, raw := range input {
			it, _ := raw.(map[string]any)
			switch it["type"] {
			case "function_call":
				args, _ := it["arguments"].(string)
				if args == "" {
					args = "{}"
				}
				call := map[string]any{"id": it["call_id"], "type": "function",
					"function": map[string]any{"name": it["name"], "arguments": args}}
				if gemini {
					id, _ := it["call_id"].(string)
					call["extra_content"] = map[string]any{"google": map[string]any{"thought_signature": signatureFor(id)}}
				}
				pending = append(pending, call)
			case "function_call_output":
				flush()
				out, ok := it["output"].(string)
				if !ok {
					b, _ := json.Marshal(it["output"])
					out = string(b)
				}
				msgs = append(msgs, map[string]any{"role": "tool", "tool_call_id": it["call_id"], "content": out})
			default: // message
				flush()
				role, _ := it["role"].(string)
				if role == "developer" {
					role = "system"
				}
				msgs = append(msgs, map[string]any{"role": role, "content": contentText(it["content"])})
			}
		}
		flush()
	default:
		return nil, errors.New("unsupported input")
	}
	// Gemini rejects requests that end with a model turn; Studio sends one
	// after an interrupted run.
	if n := len(msgs); n > 0 && msgs[n-1]["role"] == "assistant" && msgs[n-1]["tool_calls"] == nil {
		msgs = append(msgs, map[string]any{"role": "user", "content": "Continue."})
	}
	out := map[string]any{"messages": msgs, "stream": false}
	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		var ts []map[string]any
		for _, raw := range tools {
			t, _ := raw.(map[string]any)
			if t["type"] != "function" {
				continue // provider-hosted tools have no chat equivalent
			}
			fn := map[string]any{"name": t["name"], "parameters": t["parameters"]}
			if d, ok := t["description"]; ok {
				fn["description"] = d
			}
			ts = append(ts, map[string]any{"type": "function", "function": fn})
		}
		if len(ts) > 0 {
			out["tools"] = ts
			switch tc := req["tool_choice"].(type) {
			case string:
				out["tool_choice"] = tc
			case map[string]any:
				out["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": tc["name"]}}
			}
		}
	}
	if text, ok := req["text"].(map[string]any); ok {
		if f, ok := text["format"].(map[string]any); ok && f["type"] == "json_schema" {
			out["response_format"] = map[string]any{"type": "json_schema",
				"json_schema": map[string]any{"name": f["name"], "schema": f["schema"], "strict": f["strict"]}}
		}
	}
	if t, ok := req["temperature"]; ok {
		out["temperature"] = t
	}
	return out, nil
}

func contentText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, p := range v {
			if m, ok := p.(map[string]any); ok {
				if t, ok := m["text"].(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// chatToResponses translates a chat completions response into the Responses
// API shape the Studio adapter reads (output items: reasoning, message,
// function_call).
func chatToResponses(body []byte, model string) ([]byte, error) {
	var cr struct {
		ID      string `json:"id"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          *string `json:"content"`
				ReasoningContent *string `json:"reasoning_content"`
				Reasoning        *string `json:"reasoning"`
				ToolCalls        []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("decode chat completion: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("chat completion has no choices")
	}
	ch := cr.Choices[0]
	id := func(p string) string { return p + "_" + strings.ToLower(ulid.Make().String()) }
	var output []map[string]any
	reasoning := ch.Message.ReasoningContent
	if reasoning == nil {
		reasoning = ch.Message.Reasoning
	}
	if reasoning != nil && *reasoning != "" {
		output = append(output, map[string]any{"type": "reasoning", "id": id("rs"),
			"content": []map[string]any{{"type": "reasoning_text", "text": *reasoning}}})
	}
	if ch.Message.Content != nil && strings.TrimSpace(*ch.Message.Content) != "" {
		output = append(output, map[string]any{"type": "message", "id": id("msg"), "role": "assistant", "status": "completed",
			"content": []map[string]any{{"type": "output_text", "text": *ch.Message.Content, "annotations": []any{}}}})
	}
	for _, tc := range ch.Message.ToolCalls {
		// gpt-oss served over chat completions sometimes leaks its harmony
		// channel marker into the name ("view_page<|channel|>commentary").
		name, _, _ := strings.Cut(tc.Function.Name, "<|")
		callID := tc.ID
		if callID == "" {
			callID = id("call")
		}
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		output = append(output, map[string]any{"type": "function_call", "id": id("fc"), "call_id": callID,
			"name": name, "arguments": args, "status": "completed"})
	}
	res := map[string]any{"id": id("resp"), "object": "response", "status": "completed", "model": model, "output": output,
		"usage": map[string]any{"input_tokens": cr.Usage.PromptTokens, "output_tokens": cr.Usage.CompletionTokens, "total_tokens": cr.Usage.TotalTokens}}
	if ch.FinishReason == "length" {
		res["status"] = "incomplete"
		res["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	return json.Marshal(res)
}

func errorBody(msg string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg}})
	return b
}

// normalizeError gives every failure the {"error":{"message"}} shape the
// adapter reads; NVIDIA errors are RFC 7807 problem documents.
func normalizeError(status int, body []byte) []byte {
	var v struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Detail string `json:"detail"`
		Title  string `json:"title"`
	}
	if json.Unmarshal(body, &v) == nil {
		if v.Error != nil && v.Error.Message != "" {
			return body
		}
		if v.Detail != "" {
			return errorBody(v.Detail)
		}
		if v.Title != "" {
			return errorBody(v.Title)
		}
	}
	return errorBody(fmt.Sprintf("AI request failed (%d)", status))
}

// recordAICall stores one provider attempt in ai_calls. It runs after the
// response is known and off the request path; a failed insert only logs.
func (s *Server) recordAICall(turnID string, attempt int, rt aiRoute, res aiResult, callErr error, took time.Duration) {
	outcome, errText := "ok", ""
	switch {
	case callErr != nil:
		outcome, errText = "provider_error", callErr.Error()
		if strings.HasPrefix(errText, "no response within") {
			outcome = "timeout"
		}
	case res.status == http.StatusTooManyRequests:
		outcome = "rate_limited"
	case res.status >= 500:
		outcome = "provider_error"
	case res.status >= 300:
		outcome = "rejected"
	}
	if outcome != "ok" && errText == "" {
		var e struct {
			Error struct{ Message string } `json:"error"`
		}
		if json.Unmarshal(normalizeError(res.status, res.body), &e) == nil {
			errText = e.Error.Message
		}
	}
	var usage struct {
		Output []struct{ Type, Name string } `json:"output"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	tools := []string{}
	if outcome == "ok" && json.Unmarshal(res.body, &usage) == nil {
		for _, o := range usage.Output {
			if o.Type == "function_call" {
				tools = append(tools, o.Name)
			}
		}
	}
	if len(errText) > 500 {
		errText = errText[:500]
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.st.Pool.Exec(ctx, `INSERT INTO ai_calls (turn_id, attempt, provider, model, outcome, http_status, latency_ms, input_tokens, output_tokens, tool_calls, error, cache_read_tokens, cache_write_tokens)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			turnID, attempt, rt.provider, rt.model, outcome, res.status, took.Milliseconds(), usage.Usage.InputTokens, usage.Usage.OutputTokens, tools, errText, res.cache.read, res.cache.write); err != nil {
			s.log.Warn("record ai call", "err", err)
			return
		}
		s.hub.Publish("ai_call", map[string]any{"provider": rt.provider, "model": rt.model, "outcome": outcome})
	}()
}

// aiCalls lists recent AI attempts for the console (GET /v1/ai/calls).
func (s *Server) aiCalls(w http.ResponseWriter, r *http.Request) {
	rows, err := s.st.Pool.Query(r.Context(), `SELECT id, at, turn_id, attempt, provider, model, outcome, http_status, latency_ms,
		input_tokens, output_tokens, tool_calls, error, cache_read_tokens FROM ai_calls ORDER BY at DESC LIMIT 1000`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var (
			id                                     int64
			at                                     time.Time
			turn, provider, model, outcome, errTxt string
			attempt, status, latency, in, outTok   int
			cacheRead                              int
			tools                                  []string
		)
		if err := rows.Scan(&id, &at, &turn, &attempt, &provider, &model, &outcome, &status, &latency, &in, &outTok, &tools, &errTxt, &cacheRead); err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, map[string]any{"id": id, "at": at, "turn_id": turn, "attempt": attempt, "provider": provider, "model": model,
			"outcome": outcome, "http_status": status, "latency_ms": latency, "input_tokens": in, "output_tokens": outTok, "tool_calls": tools, "error": errTxt, "cache_read_tokens": cacheRead})
	}
	writeJSON(w, http.StatusOK, map[string]any{"calls": out})
}

// Gemini 3 returns a thought signature on each tool call and rejects the next
// request (400 "Function call is missing a thought_signature") when it is not
// sent back. The Studio adapter does not round-trip provider fields, so the
// proxy keeps signatures by call ID. Calls Gemini did not produce (an earlier
// step served by another provider, or a restart) use the placeholder Google
// documents for history it did not generate; verified live 2026-10-18.
const skipSignature = "skip_thought_signature_validator"

var signatures = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func rememberSignatures(chatBody []byte) {
	var cr struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					ID           string `json:"id"`
					ExtraContent struct {
						Google struct {
							ThoughtSignature string `json:"thought_signature"`
						} `json:"google"`
					} `json:"extra_content"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(chatBody, &cr) != nil || len(cr.Choices) == 0 {
		return
	}
	signatures.Lock()
	defer signatures.Unlock()
	if len(signatures.m) > 10000 { // bounded; old turns lose signatures and use the placeholder
		signatures.m = map[string]string{}
	}
	for _, tc := range cr.Choices[0].Message.ToolCalls {
		if sig := tc.ExtraContent.Google.ThoughtSignature; tc.ID != "" && sig != "" {
			signatures.m[tc.ID] = sig
		}
	}
}

func signatureFor(callID string) string {
	signatures.Lock()
	defer signatures.Unlock()
	if sig, ok := signatures.m[callID]; ok {
		return sig
	}
	return skipSignature
}

func (s *Server) claudeCall(ctx context.Context, rt aiRoute, req map[string]any, strict bool) (aiResult, error) {
	body, err := responsesToAnthropic(req, rt.model, rt.maxOut, strict)
	if err != nil {
		return aiResult{}, err
	}
	b, err := json.Marshal(body)
	if err != nil {
		return aiResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, rt.timeout)
	defer cancel()
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, rt.base+"/messages", bytes.NewReader(b))
	if err != nil {
		return aiResult{}, err
	}
	hreq.Header.Set("x-api-key", rt.key)
	hreq.Header.Set("anthropic-version", anthropicVersion)
	hreq.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(hreq)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return aiResult{}, fmt.Errorf("no response within %s", rt.timeout)
		}
		return aiResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return aiResult{}, err
	}
	res := aiResult{status: resp.StatusCode, body: out, retry: resp.Header.Get("Retry-After")}
	if resp.StatusCode < 300 {
		conv, cache, err := anthropicToResponses(out, rt.model)
		if err != nil {
			return aiResult{}, err
		}
		res.body, res.cache = conv, cache
	}
	return res, nil
}

func ulidString() string { return ulid.Make().String() }
