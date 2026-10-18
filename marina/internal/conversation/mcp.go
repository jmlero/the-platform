package conversation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
)

// MCPTools is an allow-list of read-only MCP tools behind one streamable-HTTP
// endpoint, normally agentgateway (PLAT-08). The gateway, not the actor, calls
// them, so every call keeps the turn's authority, deadline and read budget.
type MCPTools struct {
	Endpoint  string   `json:"endpoint"`
	TokenFile string   `json:"token_file,omitempty"`
	Tools     []string `json:"tools"`
}

var mcpToolName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const (
	mcpResultLimit = 16 << 10
	mcpSchemaLimit = 8 << 10
	mcpMessage     = 1 << 20
)

func (m MCPTools) Validate() error {
	endpoint, err := url.Parse(m.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("MCP tools need an HTTPS endpoint")
	}
	if len(m.Tools) < 1 || len(m.Tools) > 8 {
		return errors.New("MCP tools need one to eight allowed tool names")
	}
	for i, name := range m.Tools {
		if !mcpToolName.MatchString(name) || name == "list_repository_files" || name == "read_repository_file" || slices.Contains(m.Tools[:i], name) {
			return errors.New("MCP tool names must be unique and must not replace repository tools")
		}
	}
	return nil
}

type mcpSession struct {
	tools  *MCPTools
	client *http.Client
	token  string
	id     string
	next   int
}

func (m *MCPTools) open(ctx context.Context, client *http.Client) (*mcpSession, error) {
	s := &mcpSession{tools: m, client: client}
	if m.TokenFile != "" {
		token, err := os.ReadFile(m.TokenFile)
		if s.token = strings.TrimSpace(string(token)); err != nil || s.token == "" {
			return nil, errors.New("MCP credential unavailable")
		}
	}
	if _, err := s.rpc(ctx, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]string{"name": "marina", "version": "1"}}); err != nil {
		return nil, err
	}
	if _, err := s.rpc(ctx, "notifications/initialized", nil); err != nil {
		return nil, err
	}
	return s, nil
}

// modelTools returns the allowed tools the server offers, as Responses
// function tools. Allowed names the server does not offer are left out.
func (s *mcpSession) modelTools(ctx context.Context) ([]json.RawMessage, error) {
	raw, err := s.rpc(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var listed struct {
		Tools []struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			InputSchema json.RawMessage `json:"inputSchema"`
		} `json:"tools"`
	}
	if json.Unmarshal(raw, &listed) != nil {
		return nil, errors.New("invalid MCP tool list")
	}
	var tools []json.RawMessage
	for _, tool := range listed.Tools {
		if !slices.Contains(s.tools.Tools, tool.Name) || len(tool.InputSchema) > mcpSchemaLimit || !json.Valid(tool.InputSchema) {
			continue
		}
		description := tool.Description
		if len(description) > 1024 {
			description = description[:1024]
		}
		encoded, _ := json.Marshal(map[string]any{"type": "function", "name": tool.Name, "description": description, "parameters": tool.InputSchema, "strict": false})
		tools = append(tools, encoded)
	}
	return tools, nil
}

// call runs one allowed tool and returns its text output as untrusted data.
func (s *mcpSession) call(ctx context.Context, name, arguments string) ([]byte, error) {
	if !slices.Contains(s.tools.Tools, name) || len(arguments) > 4096 {
		return nil, errors.New("unavailable MCP tool")
	}
	var input map[string]any
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return nil, errors.New("invalid MCP tool arguments")
	}
	raw, err := s.rpc(ctx, "tools/call", map[string]any{"name": name, "arguments": input})
	if err != nil {
		return nil, err
	}
	var result struct {
		Content []struct{ Type, Text string } `json:"content"`
		IsError bool                          `json:"isError"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return nil, errors.New("invalid MCP tool result")
	}
	var text strings.Builder
	for _, part := range result.Content {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
	}
	if text.Len() > mcpResultLimit {
		return json.Marshal(map[string]any{"error": "tool result exceeds 16 KiB"})
	}
	return json.Marshal(map[string]any{"is_error": result.IsError, "text": text.String()})
}

func (s *mcpSession) rpc(ctx context.Context, method string, params any) (json.RawMessage, error) {
	message := map[string]any{"jsonrpc": "2.0", "method": method}
	notification := strings.HasPrefix(method, "notifications/")
	if !notification {
		s.next++
		message["id"] = s.next
	}
	if params != nil {
		message["params"] = params
	}
	encoded, _ := json.Marshal(message)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tools.Endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if s.token != "" {
		request.Header.Set("Authorization", "Bearer "+s.token)
	}
	if s.id != "" {
		request.Header.Set("Mcp-Session-Id", s.id)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, errors.New("MCP endpoint unavailable")
	}
	defer response.Body.Close()
	if id := response.Header.Get("Mcp-Session-Id"); id != "" && s.id == "" {
		s.id = id
	}
	if notification {
		if response.StatusCode/100 != 2 {
			return nil, errors.New("MCP endpoint rejected the session")
		}
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("MCP endpoint rejected the request")
	}
	body := io.LimitReader(response.Body, mcpMessage+1)
	var payloads [][]byte
	if strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		scanner := bufio.NewScanner(body)
		scanner.Buffer(make([]byte, 4096), mcpMessage)
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data:"); ok {
				payloads = append(payloads, []byte(strings.TrimSpace(data)))
			}
		}
	} else {
		raw, err := io.ReadAll(body)
		if err != nil || len(raw) > mcpMessage {
			return nil, errors.New("MCP response unavailable or oversized")
		}
		payloads = append(payloads, raw)
	}
	for _, payload := range payloads {
		var reply struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct{}       `json:"error"`
		}
		if json.Unmarshal(payload, &reply) != nil || reply.ID != s.next {
			continue
		}
		if reply.Error != nil || reply.Result == nil {
			return nil, errors.New("MCP request failed")
		}
		return reply.Result, nil
	}
	return nil, errors.New("MCP response missing")
}
