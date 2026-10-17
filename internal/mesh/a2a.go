// Package mesh implements Mandate's agent-to-agent layer: a minimal,
// spec-faithful subset of the A2A protocol 1.0 JSON-RPC binding
// (https://a2a-protocol.org/latest/specification/): Agent Cards and a
// synchronous SendMessage whose request and response carry structured JSON
// in "data" parts.
package mesh

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ProtocolVersion = "1.0"

type Part struct {
	Text      string          `json:"text,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
}

type Message struct {
	MessageID string `json:"messageId"`
	ContextID string `json:"contextId,omitempty"`
	Role      string `json:"role"` // ROLE_USER | ROLE_AGENT
	Parts     []Part `json:"parts"`
}

type AgentInterface struct {
	URL             string `json:"url"`
	ProtocolBinding string `json:"protocolBinding"`
	ProtocolVersion string `json:"protocolVersion"`
}

type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	InputModes  []string `json:"inputModes"`
	OutputModes []string `json:"outputModes"`
}

type AgentCard struct {
	Name                string           `json:"name"`
	Description         string           `json:"description"`
	SupportedInterfaces []AgentInterface `json:"supportedInterfaces"`
	Provider            map[string]any   `json:"provider"`
	Version             string           `json:"version"`
	Capabilities        map[string]bool  `json:"capabilities"`
	DefaultInputModes   []string         `json:"defaultInputModes"`
	DefaultOutputModes  []string         `json:"defaultOutputModes"`
	Skills              []AgentSkill     `json:"skills"`
	// Mandate extension: the key the supplier signs quotes with. Mandates
	// pin this key in their payee allowlist; it is never trusted from a
	// card at payment time.
	Metadata map[string]any `json:"metadata,omitempty"`
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// JSON-RPC 2.0 error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

func NewMessageID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// DataPart wraps v as a structured JSON part.
func DataPart(v any) (Part, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Part{}, err
	}
	return Part{Data: b, MediaType: "application/json"}, nil
}

// FirstData decodes the first data part of a message into v.
func FirstData(m Message, v any) error {
	for _, p := range m.Parts {
		if len(p.Data) > 0 {
			return json.Unmarshal(p.Data, v)
		}
	}
	return errors.New("a2a: message has no data part")
}

// Handler serves SendMessage for one agent.
type Handler func(ctx context.Context, in Message) (Message, error)

// ServeRPC returns an http.Handler implementing the JSON-RPC binding.
func ServeRPC(h Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(id any, result any, e *rpcError) {
			resp := rpcResponse{JSONRPC: "2.0", ID: id, Error: e}
			if result != nil {
				resp.Result, _ = json.Marshal(result)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}
		var req rpcRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			reply(nil, nil, &rpcError{Code: codeParse, Message: "parse error"})
			return
		}
		if req.JSONRPC != "2.0" {
			reply(req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "jsonrpc must be 2.0"})
			return
		}
		if req.Method != "SendMessage" {
			reply(req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + req.Method})
			return
		}
		var params struct {
			Message Message `json:"message"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || params.Message.MessageID == "" || len(params.Message.Parts) == 0 {
			reply(req.ID, nil, &rpcError{Code: codeInvalidParams, Message: "SendMessage requires a message with messageId and parts"})
			return
		}
		out, err := h(r.Context(), params.Message)
		if err != nil {
			reply(req.ID, nil, &rpcError{Code: codeInternal, Message: err.Error()})
			return
		}
		reply(req.ID, map[string]any{"message": out}, nil)
	})
}

// Client calls a remote agent's JSON-RPC interface.
type Client struct {
	HTTP *http.Client
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c Client) FetchCard(ctx context.Context, cardURL string) (AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cardURL, nil)
	if err != nil {
		return AgentCard{}, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return AgentCard{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return AgentCard{}, fmt.Errorf("a2a: agent card %s returned %d", cardURL, resp.StatusCode)
	}
	var card AgentCard
	return card, json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&card)
}

// JSONRPCEndpoint picks the card's JSON-RPC interface.
func (card AgentCard) JSONRPCEndpoint() (string, error) {
	for _, i := range card.SupportedInterfaces {
		if i.ProtocolBinding == "JSONRPC" {
			return i.URL, nil
		}
	}
	return "", errors.New("a2a: agent card has no JSONRPC interface")
}

func (c Client) Send(ctx context.Context, endpoint string, msg Message) (Message, error) {
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: msg.MessageID, Method: "SendMessage",
		Params: mustJSON(map[string]any{"message": msg})})
	if err != nil {
		return Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", ProtocolVersion)
	resp, err := c.http().Do(req)
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var out rpcResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Message{}, err
	}
	if out.Error != nil {
		return Message{}, fmt.Errorf("a2a: %d %s", out.Error.Code, out.Error.Message)
	}
	var result struct {
		Message Message `json:"message"`
	}
	if err := json.Unmarshal(out.Result, &result); err != nil {
		return Message{}, err
	}
	return result.Message, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func jsonEncode(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
