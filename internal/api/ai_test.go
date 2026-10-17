package api

import (
	"encoding/json"
	"testing"
)

func TestResponsesToChat(t *testing.T) {
	var req map[string]any
	if err := json.Unmarshal([]byte(`{
		"instructions": "be brief",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]},
			{"type": "function_call", "call_id": "c1", "name": "a", "arguments": "{\"x\":1}"},
			{"type": "function_call", "call_id": "c2", "name": "b", "arguments": ""},
			{"type": "function_call_output", "call_id": "c1", "output": "one"},
			{"type": "function_call_output", "call_id": "c2", "output": {"ok": true}},
			{"type": "message", "role": "developer", "content": [{"type": "input_text", "text": "note"}]}
		],
		"tools": [{"type": "function", "name": "a", "description": "d", "parameters": {"type": "object"}}, {"type": "web_search"}],
		"tool_choice": {"type": "function", "name": "a"},
		"text": {"format": {"type": "json_schema", "name": "out", "schema": {"type": "object"}, "strict": false}}
	}`), &req); err != nil {
		t.Fatal(err)
	}
	chat, err := responsesToChat(req, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(chat)
	var got struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID       string `json:"id"`
				Function struct{ Name, Arguments string }
			} `json:"tool_calls"`
		} `json:"messages"`
		Tools          []map[string]any `json:"tools"`
		ToolChoice     map[string]any   `json:"tool_choice"`
		ResponseFormat map[string]any   `json:"response_format"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	roles := ""
	for _, m := range got.Messages {
		roles += m.Role + ","
	}
	if roles != "system,user,assistant,tool,tool,system," {
		t.Fatalf("roles = %s", roles)
	}
	asst := got.Messages[2]
	if len(asst.ToolCalls) != 2 || asst.ToolCalls[0].ID != "c1" || asst.ToolCalls[1].Function.Arguments != "{}" {
		t.Fatalf("tool calls = %+v", asst.ToolCalls)
	}
	if got.Messages[4].ToolCallID != "c2" || got.Messages[4].Content != `{"ok":true}` {
		t.Fatalf("tool output = %+v", got.Messages[4])
	}
	if len(got.Tools) != 1 || got.ToolChoice["type"] != "function" || got.ResponseFormat["type"] != "json_schema" {
		t.Fatalf("tools=%v choice=%v format=%v", got.Tools, got.ToolChoice, got.ResponseFormat)
	}
}

func TestChatToResponses(t *testing.T) {
	in := []byte(`{"choices":[{"finish_reason":"tool_calls","message":{"content":"thinking aloud","reasoning_content":"why",
		"tool_calls":[{"id":"call_9","function":{"name":"view_page<|channel|>commentary","arguments":""}}]}}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	out, err := chatToResponses(in, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Status string `json:"status"`
		Output []struct {
			Type, Name, Arguments string
			CallID                string `json:"call_id"`
		} `json:"output"`
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "completed" || len(got.Output) != 3 || got.Usage.InputTokens != 10 {
		t.Fatalf("got %+v", got)
	}
	fc := got.Output[2]
	if fc.Type != "function_call" || fc.Name != "view_page" || fc.CallID != "call_9" || fc.Arguments != "{}" {
		t.Fatalf("function call = %+v", fc)
	}
}

func TestChatToResponsesLength(t *testing.T) {
	out, err := chatToResponses([]byte(`{"choices":[{"finish_reason":"length","message":{"content":"cut"}}]}`), "m")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["status"] != "incomplete" {
		t.Fatalf("status = %v", got["status"])
	}
}

func TestNormalizeError(t *testing.T) {
	if got := string(normalizeError(404, []byte(`{"status":404,"title":"Not Found","detail":"Function not found"}`))); got != `{"error":{"message":"Function not found"}}` {
		t.Fatalf("got %s", got)
	}
	if got := string(normalizeError(502, []byte(`<html>`))); got != `{"error":{"message":"AI request failed (502)"}}` {
		t.Fatalf("got %s", got)
	}
}

func TestGeminiSignatures(t *testing.T) {
	rememberSignatures([]byte(`{"choices":[{"message":{"tool_calls":[{"id":"g1","extra_content":{"google":{"thought_signature":"SIG"}}}]}}]}`))
	var req map[string]any
	_ = json.Unmarshal([]byte(`{"input":[
		{"type":"function_call","call_id":"g1","name":"a","arguments":"{}"},
		{"type":"function_call","call_id":"other","name":"a","arguments":"{}"},
		{"type":"function_call_output","call_id":"g1","output":"x"},
		{"type":"function_call_output","call_id":"other","output":"y"}]}`), &req)
	chat, err := responsesToChat(req, true)
	if err != nil {
		t.Fatal(err)
	}
	calls := chat["messages"].([]map[string]any)[0]["tool_calls"].([]map[string]any)
	sig := func(i int) any {
		return calls[i]["extra_content"].(map[string]any)["google"].(map[string]any)["thought_signature"]
	}
	if sig(0) != "SIG" || sig(1) != skipSignature {
		t.Fatalf("signatures = %v, %v", sig(0), sig(1))
	}
}

func TestResponsesToAnthropic(t *testing.T) {
	var req map[string]any
	_ = json.Unmarshal([]byte(`{
		"instructions": "be brief",
		"input": [
			{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "hi"}]},
			{"type": "message", "role": "assistant", "content": [{"type": "output_text", "text": "checking"}]},
			{"type": "function_call", "call_id": "call-a.1", "name": "a", "arguments": "{\"x\":1}"},
			{"type": "function_call", "call_id": "c2", "name": "b", "arguments": ""},
			{"type": "function_call_output", "call_id": "call-a.1", "output": "one"},
			{"type": "function_call_output", "call_id": "c2", "output": {"ok": true}}
		],
		"tools": [
			{"type": "function", "name": "a", "parameters": {"type": "object", "properties": {"x": {"type": "integer"}}, "required": ["x"], "additionalProperties": false}},
			{"type": "function", "name": "b", "parameters": {"type": "object", "properties": {"n": {"type": "integer", "minimum": 1}}, "additionalProperties": false}}
		],
		"tool_choice": "required"
	}`), &req)
	out, err := responsesToAnthropic(req, "claude-haiku-5-5", 1000, true)
	if err != nil {
		t.Fatal(err)
	}
	msgs := out["messages"].([]anthropicMsg)
	if len(msgs) != 3 || msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Fatalf("messages = %+v", msgs)
	}
	if len(msgs[1].Content) != 3 || msgs[1].Content[1]["id"] != "call-a_1" || msgs[2].Content[0]["tool_use_id"] != "call-a_1" {
		t.Fatalf("tool turn = %+v / %+v", msgs[1].Content, msgs[2].Content)
	}
	if msgs[2].Content[1]["content"] != `{"ok":true}` {
		t.Fatalf("tool result = %v", msgs[2].Content[1])
	}
	tools := out["tools"].([]map[string]any)
	if tools[0]["strict"] != true || tools[1]["strict"] != nil {
		t.Fatalf("strict flags = %v, %v", tools[0]["strict"], tools[1]["strict"])
	}
	if out["system"] != "be brief" || out["tool_choice"].(map[string]any)["type"] != "any" {
		t.Fatalf("system=%v choice=%v", out["system"], out["tool_choice"])
	}
}

func TestAnthropicToResponses(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"ok"},{"type":"tool_use","id":"toolu_1","name":"a","input":{"x":1}}],
		"stop_reason":"tool_use","usage":{"input_tokens":10,"cache_read_input_tokens":90,"cache_creation_input_tokens":0,"output_tokens":5}}`)
	out, cache, err := anthropicToResponses(body, "m")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Output []struct {
			Type, Name, Arguments string
			CallID                string `json:"call_id"`
		}
		Usage struct {
			InputTokens int `json:"input_tokens"`
		}
	}
	_ = json.Unmarshal(out, &got)
	if len(got.Output) != 2 || got.Output[1].CallID != "toolu_1" || got.Output[1].Arguments != `{"x":1}` || got.Usage.InputTokens != 100 || cache.read != 90 {
		t.Fatalf("got %+v cache %+v", got, cache)
	}
}

func TestTrailingAssistantTurnGetsUserContinue(t *testing.T) {
	var req map[string]any
	_ = json.Unmarshal([]byte(`{"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}]}`), &req)
	a, _ := responsesToAnthropic(req, "m", 10, true)
	msgs := a["messages"].([]anthropicMsg)
	if msgs[len(msgs)-1].Role != "user" {
		t.Fatalf("claude last role = %s", msgs[len(msgs)-1].Role)
	}
	c, _ := responsesToChat(req, true)
	cm := c["messages"].([]map[string]any)
	if cm[len(cm)-1]["role"] != "user" {
		t.Fatalf("chat last role = %v", cm[len(cm)-1]["role"])
	}
}

func TestStrictSkipsRefs(t *testing.T) {
	if strictCompatible(map[string]any{"type": "object", "additionalProperties": false,
		"properties": map[string]any{"e": map[string]any{"$ref": "#/$defs/E"}}}) {
		t.Fatal("schema with $ref should not be strict")
	}
}
