// Package mcp is enough of the Model Context Protocol to use Home Assistant's MCP server
// (/api/mcp, streamable HTTP): start a session, list its tools, call one. Realtime voice offers those
// tools to its model, so it can do in the house what Assist can.
//
// The server answers each request either as plain JSON or as a stream of server-sent events; both are
// read, to the answer with the request's id. Every answer is bounded in size and time, and a tool's name
// is held to what a name looks like, since the names go on to a model and into the log.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// protocolVersion is the version asked for; the server answers with the one it speaks.
	protocolVersion = "2025-03-26"

	// maxAnswer is the most one answer may be: a house's tool list is tens of kilobytes.
	maxAnswer = 1 << 20

	// maxTools is the most tools taken from a server.
	maxTools = 128

	// callWithin is how long one request may take.
	callWithin = 15 * time.Second
)

// Tool is one of the server's tools.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations struct {
		ReadOnlyHint *bool `json:"readOnlyHint"`
	} `json:"annotations"`
}

// ReadOnly is whether the server says the tool only looks something up.
func (t Tool) ReadOnly() bool {
	return t.Annotations.ReadOnlyHint != nil && *t.Annotations.ReadOnlyHint
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Client is one session with one server.
type Client struct {
	URL   string // like http://homeassistant.local:8123/api/mcp
	Token string
	HTTP  *http.Client

	mu      sync.Mutex
	session string // the server's session id, once it gave one
	version string
	nextID  int
}

// New is a client for a server's address and token.
func New(url, token string) *Client {
	return &Client{URL: url, Token: token, HTTP: &http.Client{
		Timeout: callWithin,
		// The token goes to the address given, never on to one a redirect names.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type response struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Start opens the session.
func (c *Client) Start(ctx context.Context) error {
	var r struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "techo5", "version": "1"},
	}, &r)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.version = r.ProtocolVersion
	c.mu.Unlock()
	return c.notify(ctx, "notifications/initialized")
}

// Tools is the server's tools, those with names that look like names, at most maxTools.
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	var r struct {
		Tools []Tool `json:"tools"`
	}
	if err := c.call(ctx, "tools/list", map[string]any{}, &r); err != nil {
		return nil, err
	}
	var out []Tool
	for _, t := range r.Tools {
		if !toolName.MatchString(t.Name) {
			continue
		}
		if len(out) == maxTools {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// Call runs a tool, answering its text and whether the server says the call failed.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (text string, failed bool, err error) {
	if !toolName.MatchString(name) {
		return "", true, fmt.Errorf("not a tool name: %q", name)
	}
	if args == nil {
		args = map[string]any{}
	}
	var r struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &r); err != nil {
		return "", true, err
	}
	var b strings.Builder
	for _, part := range r.Content {
		if part.Type == "text" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(part.Text)
		}
	}
	return b.String(), r.IsError, nil
}

// call sends a request and reads its answer into out.
func (c *Client) call(ctx context.Context, method string, params, out any) error {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()
	raw, err := c.post(ctx, request{JSONRPC: "2.0", ID: &id, Method: method, Params: params}, id)
	if err != nil {
		return err
	}
	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("mcp: %s: not an answer: %w", method, err)
	}
	if r.Error != nil {
		return fmt.Errorf("mcp: %s: %s", method, r.Error.Message)
	}
	if out != nil && len(r.Result) > 0 {
		if err := json.Unmarshal(r.Result, out); err != nil {
			return fmt.Errorf("mcp: %s: %w", method, err)
		}
	}
	return nil
}

func (c *Client) notify(ctx context.Context, method string) error {
	_, err := c.post(ctx, request{JSONRPC: "2.0", Method: method}, 0)
	return err
}

// post sends one message, and answers the response with that id (id 0 for a notification, which has
// none).
func (c *Client) post(ctx context.Context, m request, id int) ([]byte, error) {
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callWithin)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	c.mu.Lock()
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	if c.version != "" {
		req.Header.Set("Mcp-Protocol-Version", c.version)
	}
	c.mu.Unlock()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if s := resp.Header.Get("Mcp-Session-Id"); s != "" && len(s) <= 256 {
		c.mu.Lock()
		c.session = s
		c.mu.Unlock()
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("mcp: the token was refused")
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("mcp: %s", resp.Status)
	}
	if id == 0 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))
		return nil, nil
	}
	body0 := io.LimitReader(resp.Body, maxAnswer+1)
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return fromEvents(body0, id)
	}
	b, err := io.ReadAll(body0)
	if err != nil {
		return nil, err
	}
	if len(b) > maxAnswer {
		return nil, errors.New("mcp: an answer too long")
	}
	return b, nil
}

// fromEvents reads server-sent events to the answer with the id.
func fromEvents(r io.Reader, id int) ([]byte, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxAnswer)
	var data bytes.Buffer
	flush := func() []byte {
		defer data.Reset()
		if data.Len() == 0 {
			return nil
		}
		var probe struct {
			ID *int `json:"id"`
		}
		if json.Unmarshal(data.Bytes(), &probe) == nil && probe.ID != nil && *probe.ID == id {
			return append([]byte(nil), data.Bytes()...)
		}
		return nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if b := flush(); b != nil {
				return b, nil
			}
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if b := flush(); b != nil {
		return b, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("mcp: the stream ended with no answer")
}
