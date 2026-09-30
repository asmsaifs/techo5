// Package llm asks a chat model behind an OpenAI-style endpoint (/v1/chat/completions), with tools:
// llama.cpp's server, Ollama, vLLM and the rest all speak it. Only what a voice turn needs is here.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Message is one entry in the conversation.
type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is the model asking for one of the tools.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON, as the model wrote it
	} `json:"function"`
}

// Tool is something the model may call: a name, what it is for, and its arguments as JSON Schema.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Client is one endpoint.
type Client struct {
	Base  string // like http://192.168.1.20:8080/v1
	Key   string
	Model string
	HTTP  *http.Client
}

// timeout is how long one answer may take: a model on a slow machine thinks for a while, but a voice
// turn nobody answers for this long is over.
const timeout = 60 * time.Second

// maxAnswer bounds what is read back.
const maxAnswer = 1 << 20

type toolJSON struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

// Chat sends the conversation so far and returns the model's next message, which is either words or
// tool calls.
func (c *Client) Chat(ctx context.Context, msgs []Message, tools []Tool) (Message, error) {
	// Low: a voice turn wants the same tool for the same request every time, not variety.
	req := map[string]any{"messages": msgs, "temperature": 0.2}
	if c.Model != "" {
		req["model"] = c.Model
	}
	if len(tools) > 0 {
		ts := make([]toolJSON, len(tools))
		for i, t := range tools {
			ts[i].Type = "function"
			ts[i].Function.Name, ts[i].Function.Description, ts[i].Function.Parameters = t.Name, t.Description, t.Parameters
		}
		req["tools"] = ts
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(c.Base), bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		r.Header.Set("Authorization", "Bearer "+c.Key)
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(r)
	if err != nil {
		return Message{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		return Message{}, err
	}
	if resp.StatusCode != http.StatusOK {
		// The body says why, but never the key: it is not in anything the server sends back.
		return Message{}, fmt.Errorf("llm: %s: %.200s", resp.Status, strings.TrimSpace(string(b)))
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return Message{}, fmt.Errorf("llm: not an answer: %w", err)
	}
	if len(out.Choices) == 0 {
		return Message{}, errors.New("llm: no answer")
	}
	m := out.Choices[0].Message
	m.Role = "assistant"
	return m, nil
}

// endpoint is the chat address for a base. A base given as just the server (http://host:8080), as
// people often type it, means the usual /v1 under it.
func endpoint(base string) string {
	base = strings.TrimRight(base, "/")
	if u, err := url.Parse(base); err == nil && u.Path == "" {
		base += "/v1"
	}
	return base + "/chat/completions"
}
