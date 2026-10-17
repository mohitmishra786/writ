package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Tool is a function the model may call. Handlers receive untrusted
// arguments: tool calls cannot be combined with Groq structured outputs, so
// every handler (and ultimately the Mandate kernel) validates its own input.
type Tool struct {
	Spec    ToolSpec
	Handler func(ctx context.Context, args json.RawMessage) (string, error)
}

// Step is one model turn, recorded for the audit trail.
type Step struct {
	Reasoning string          `json:"reasoning,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolArgs  json.RawMessage `json:"tool_args,omitempty"`
	ToolOut   string          `json:"tool_output,omitempty"`
	ToolErr   string          `json:"tool_error,omitempty"`
	Tokens    int             `json:"tokens"`
}

type Agent struct {
	Client          *Client
	Model           string
	System          string
	Tools           []Tool
	ReasoningEffort string
	MaxSteps        int
}

type Result struct {
	Final string
	Steps []Step
}

var ErrMaxSteps = errors.New("llm: agent exceeded max steps")

// Run drives the tool loop. Tool errors are returned to the model as tool
// output (so it can recover, e.g. after a policy denial) and recorded.
func (a Agent) Run(ctx context.Context, user string) (Result, error) {
	byName := make(map[string]Tool, len(a.Tools))
	specs := make([]ToolSpec, len(a.Tools))
	for i, t := range a.Tools {
		byName[t.Spec.Name] = t
		specs[i] = t.Spec
	}
	maxSteps := a.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 8
	}
	msgs := []Message{{Role: "system", Content: a.System}, {Role: "user", Content: user}}
	var res Result
	for range maxSteps {
		resp, err := a.Client.Complete(ctx, Request{
			Model: a.Model, Messages: msgs, Tools: specs, ReasoningEffort: a.ReasoningEffort,
		})
		if err != nil {
			return res, err
		}
		m := resp.Message
		if len(m.ToolCalls) == 0 {
			res.Final = m.Content
			res.Steps = append(res.Steps, Step{Reasoning: m.Reasoning, Content: m.Content, Tokens: resp.Usage.TotalTokens})
			return res, nil
		}
		msgs = append(msgs, Message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		for _, call := range m.ToolCalls {
			step := Step{Reasoning: m.Reasoning, ToolName: call.Function.Name, ToolArgs: json.RawMessage(call.Function.Arguments), Tokens: resp.Usage.TotalTokens}
			out, err := a.invoke(ctx, byName, call)
			if err != nil {
				step.ToolErr = err.Error()
				out = fmt.Sprintf(`{"error":%q}`, err.Error())
			} else {
				step.ToolOut = out
			}
			res.Steps = append(res.Steps, step)
			msgs = append(msgs, Message{Role: "tool", ToolCallID: call.ID, Content: out})
		}
	}
	return res, ErrMaxSteps
}

func (a Agent) invoke(ctx context.Context, byName map[string]Tool, call ToolCall) (string, error) {
	t, ok := byName[call.Function.Name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", call.Function.Name)
	}
	args := json.RawMessage(call.Function.Arguments)
	if !json.Valid(args) {
		return "", errors.New("tool arguments are not valid JSON")
	}
	return t.Handler(ctx, args)
}
