package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Claude through the native Messages API (https://platform.claude.com/docs/en/api/messages).
// Anthropic describes its OpenAI-compatible endpoint as a testing aid that
// ignores `strict` and has no prompt caching, and both matter here: strict
// tool use guarantees schema-valid arguments (the failure mode of the
// NVIDIA models on Studio's delegate_to tool), and Studio resends ~5K tokens
// of instructions and tool definitions on every call, which caching serves
// at 10% of the input price.
//
// Settings follow the Haiku 5.5 guidance: effort "medium" for agentic tool
// use, thinking disabled (allowed at medium) because the Studio adapter does
// not round-trip thinking blocks, automatic prompt caching via a top-level
// cache_control.

const anthropicVersion = "2023-06-01"

var toolIDUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// anthropicToolID maps any call ID to the charset tool_use ids accept; the
// same mapping is applied to tool_result ids so pairs still match.
func anthropicToolID(id any) string {
	s, _ := id.(string)
	if s == "" {
		return "call_missing"
	}
	return toolIDUnsafe.ReplaceAllString(s, "_")
}

type anthropicMsg struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

// responsesToAnthropic translates a Responses API request into a Messages
// API request. strict selects schema-constrained tools where the schema fits
// the supported subset.
func responsesToAnthropic(req map[string]any, model string, maxOut int, strict bool) (map[string]any, error) {
	var system []string
	if ins, _ := req["instructions"].(string); ins != "" {
		system = append(system, ins)
	}
	var msgs []anthropicMsg
	add := func(role string, block map[string]any) {
		if n := len(msgs); n > 0 && msgs[n-1].Role == role {
			msgs[n-1].Content = append(msgs[n-1].Content, block)
			return
		}
		msgs = append(msgs, anthropicMsg{Role: role, Content: []map[string]any{block}})
	}
	switch input := req["input"].(type) {
	case string:
		add("user", map[string]any{"type": "text", "text": input})
	case []any:
		for _, raw := range input {
			it, _ := raw.(map[string]any)
			switch it["type"] {
			case "function_call":
				args := map[string]any{}
				if s, _ := it["arguments"].(string); strings.TrimSpace(s) != "" {
					if err := json.Unmarshal([]byte(s), &args); err != nil {
						args = map[string]any{}
					}
				}
				add("assistant", map[string]any{"type": "tool_use", "id": anthropicToolID(it["call_id"]), "name": it["name"], "input": args})
			case "function_call_output":
				out, ok := it["output"].(string)
				if !ok {
					b, _ := json.Marshal(it["output"])
					out = string(b)
				}
				add("user", map[string]any{"type": "tool_result", "tool_use_id": anthropicToolID(it["call_id"]), "content": out})
			default: // message
				text := contentText(it["content"])
				switch it["role"] {
				case "system", "developer":
					system = append(system, text)
				case "assistant":
					if strings.TrimSpace(text) != "" {
						add("assistant", map[string]any{"type": "text", "text": text})
					}
				default:
					if strings.TrimSpace(text) != "" {
						add("user", map[string]any{"type": "text", "text": text})
					}
				}
			}
		}
	default:
		return nil, errors.New("unsupported input")
	}
	if len(msgs) == 0 || msgs[0].Role != "user" {
		msgs = append([]anthropicMsg{{Role: "user", Content: []map[string]any{{"type": "text", "text": "Continue."}}}}, msgs...)
	}
	// Claude 5 models reject a conversation that ends with an assistant turn
	// (no prefill); Studio sends one after an interrupted run.
	if msgs[len(msgs)-1].Role == "assistant" {
		msgs = append(msgs, anthropicMsg{Role: "user", Content: []map[string]any{{"type": "text", "text": "Continue."}}})
	}

	out := map[string]any{
		"model":         model,
		"max_tokens":    maxOut,
		"messages":      msgs,
		"thinking":      map[string]any{"type": "disabled"},
		"cache_control": map[string]any{"type": "ephemeral"},
	}
	if len(system) > 0 {
		out["system"] = strings.Join(system, "\n\n")
	}
	outputConfig := map[string]any{"effort": "medium"}

	if tools, ok := req["tools"].([]any); ok && len(tools) > 0 {
		var ts []map[string]any
		budget := strictBudget{tools: 20, optional: 24, unions: 16}
		for _, raw := range tools {
			t, _ := raw.(map[string]any)
			if t["type"] != "function" {
				continue
			}
			schema, _ := t["parameters"].(map[string]any)
			if schema == nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tool := map[string]any{"name": t["name"], "input_schema": schema}
			if d, ok := t["description"].(string); ok && d != "" {
				tool["description"] = d
			}
			if strict && budget.take(schema) {
				tool["strict"] = true
			}
			ts = append(ts, tool)
		}
		if len(ts) > 0 {
			out["tools"] = ts
			switch tc := req["tool_choice"].(type) {
			case string:
				switch tc {
				case "required":
					out["tool_choice"] = map[string]any{"type": "any"}
				case "none":
					out["tool_choice"] = map[string]any{"type": "none"}
				default:
					out["tool_choice"] = map[string]any{"type": "auto"}
				}
			case map[string]any:
				out["tool_choice"] = map[string]any{"type": "tool", "name": tc["name"]}
			}
		}
	}
	if text, ok := req["text"].(map[string]any); ok {
		if f, ok := text["format"].(map[string]any); ok && f["type"] == "json_schema" {
			if schema, ok := f["schema"].(map[string]any); ok && strictCompatible(schema) {
				outputConfig["format"] = map[string]any{"type": "json_schema", "schema": schema}
			}
		}
	}
	out["output_config"] = outputConfig
	return out, nil
}

// strictBudget tracks the documented per-request limits across all strict
// schemas: 20 strict tools, 24 optional parameters, 16 union parameters.
type strictBudget struct{ tools, optional, unions int }

func (b *strictBudget) take(schema map[string]any) bool {
	if b.tools == 0 || !strictCompatible(schema) {
		return false
	}
	opt, uni := schemaCounts(schema)
	if opt > b.optional || uni > b.unions {
		return false
	}
	b.tools--
	b.optional -= opt
	b.unions -= uni
	return true
}

// strictCompatible reports whether a schema stays inside the structured
// outputs subset: no numeric or string length constraints, array minItems
// only 0 or 1, and objects closed with additionalProperties false.
func strictCompatible(v any) bool {
	switch s := v.(type) {
	case map[string]any:
		// $ref is allowed by the subset but Studio's widget schemas use
		// recursive definitions, which strict rejects; skip any schema with refs.
		for _, k := range []string{"$ref", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf",
			"minLength", "maxLength", "maxItems", "oneOf", "not", "patternProperties", "if", "then", "else"} {
			if _, ok := s[k]; ok {
				return false
			}
		}
		if mi, ok := s["minItems"].(float64); ok && mi > 1 {
			return false
		}
		if _, isObj := s["properties"]; isObj || s["type"] == "object" {
			if ap, ok := s["additionalProperties"]; !ok || ap != false {
				return false
			}
		}
		for k, child := range s {
			if k == "enum" || k == "const" || k == "default" || k == "description" || k == "examples" {
				continue
			}
			if !strictCompatible(child) {
				return false
			}
		}
	case []any:
		for _, child := range s {
			if !strictCompatible(child) {
				return false
			}
		}
	}
	return true
}

// schemaCounts returns the number of optional properties and union-typed
// properties anywhere in the schema.
func schemaCounts(v any) (optional, unions int) {
	s, ok := v.(map[string]any)
	if !ok {
		if arr, ok := v.([]any); ok {
			for _, c := range arr {
				o, u := schemaCounts(c)
				optional, unions = optional+o, unions+u
			}
		}
		return
	}
	if props, ok := s["properties"].(map[string]any); ok {
		req := map[string]bool{}
		if r, ok := s["required"].([]any); ok {
			for _, x := range r {
				if name, ok := x.(string); ok {
					req[name] = true
				}
			}
		}
		for name, p := range props {
			if !req[name] {
				optional++
			}
			if pm, ok := p.(map[string]any); ok {
				if _, ok := pm["anyOf"]; ok {
					unions++
				}
				if _, ok := pm["type"].([]any); ok {
					unions++
				}
			}
		}
	}
	for k, child := range s {
		if k == "enum" || k == "const" || k == "default" {
			continue
		}
		o, u := schemaCounts(child)
		optional, unions = optional+o, unions+u
	}
	return
}

// anthropicToResponses translates a Messages API response into the
// Responses API shape the Studio adapter reads.
func anthropicToResponses(body []byte, model string) ([]byte, cacheUsage, error) {
	var mr struct {
		Content []struct {
			Type     string          `json:"type"`
			Text     string          `json:"text"`
			Thinking string          `json:"thinking"`
			ID       string          `json:"id"`
			Name     string          `json:"name"`
			Input    json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &mr); err != nil {
		return nil, cacheUsage{}, fmt.Errorf("decode message: %w", err)
	}
	var output []map[string]any
	n := 0
	id := func(p string) string { n++; return fmt.Sprintf("%s_%d_%s", p, n, strings.ToLower(ulidString())) }
	for _, c := range mr.Content {
		switch c.Type {
		case "thinking":
			if c.Thinking != "" {
				output = append(output, map[string]any{"type": "reasoning", "id": id("rs"),
					"content": []map[string]any{{"type": "reasoning_text", "text": c.Thinking}}})
			}
		case "text":
			if strings.TrimSpace(c.Text) != "" {
				output = append(output, map[string]any{"type": "message", "id": id("msg"), "role": "assistant", "status": "completed",
					"content": []map[string]any{{"type": "output_text", "text": c.Text, "annotations": []any{}}}})
			}
		case "tool_use":
			args := string(c.Input)
			if args == "" || args == "null" {
				args = "{}"
			}
			output = append(output, map[string]any{"type": "function_call", "id": id("fc"), "call_id": c.ID,
				"name": c.Name, "arguments": args, "status": "completed"})
		}
	}
	in := mr.Usage.InputTokens + mr.Usage.CacheReadInputTokens + mr.Usage.CacheCreationInputTokens
	res := map[string]any{"id": id("resp"), "object": "response", "status": "completed", "model": model, "output": output,
		"usage": map[string]any{"input_tokens": in, "output_tokens": mr.Usage.OutputTokens, "total_tokens": in + mr.Usage.OutputTokens}}
	if mr.StopReason == "max_tokens" {
		res["status"] = "incomplete"
		res["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	b, err := json.Marshal(res)
	return b, cacheUsage{read: mr.Usage.CacheReadInputTokens, write: mr.Usage.CacheCreationInputTokens}, err
}

type cacheUsage struct{ read, write int }

// isStrictSchemaError reports a 400 caused by a tool schema the strict
// grammar compiler rejected; the call is retried without strict.
func isStrictSchemaError(status int, body []byte) bool {
	if status != 400 {
		return false
	}
	b := strings.ToLower(string(body))
	return strings.Contains(b, "schema") && (strings.Contains(b, "strict") || strings.Contains(b, "too complex") || strings.Contains(b, "not supported") || strings.Contains(b, "unsupported"))
}
