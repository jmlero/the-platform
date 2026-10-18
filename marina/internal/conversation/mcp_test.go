package conversation

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMCP is a streamable-HTTP MCP server with one allowed and one other tool.
type fakeMCP struct {
	mu      sync.Mutex
	calls   []string
	headers []string
}

func (m *fakeMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     *int            `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	body, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(body, &request)
	m.mu.Lock()
	m.headers = append(m.headers, r.Header.Get("Authorization")+"|"+r.Header.Get("Mcp-Session-Id"))
	m.mu.Unlock()
	if request.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	result := `{}`
	switch request.Method {
	case "initialize":
		w.Header().Set("Mcp-Session-Id", "session-1")
		result = `{"protocolVersion":"2025-06-18","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"1"}}`
	case "tools/list":
		result = `{"tools":[{"name":"search_traces","description":"Find traces","inputSchema":{"type":"object","properties":{"service":{"type":"string"}}}},{"name":"delete_everything","description":"Not allowed","inputSchema":{"type":"object"}}]}`
	case "tools/call":
		var params struct{ Name string }
		_ = json.Unmarshal(request.Params, &params)
		m.mu.Lock()
		m.calls = append(m.calls, params.Name)
		m.mu.Unlock()
		result = `{"content":[{"type":"text","text":"3 traces for marina"}],"isError":false}`
	}
	// Answer as an event stream, as streamable-HTTP servers may.
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":"+strings.TrimSpace(string(mustJSON(*request.ID)))+",\"result\":"+result+"}\n\n")
}

func mustJSON(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func TestAllowedMCPToolResultsReachTheModelAndOthersAreHidden(t *testing.T) {
	server := &fakeMCP{}
	mcp := httptest.NewTLSServer(server)
	defer mcp.Close()
	calls := 0
	g, _, call := repositoryGateway(t, 20000, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == strings.TrimPrefix(mcp.URL, "https://") {
			return mcp.Client().Transport.RoundTrip(r)
		}
		calls++
		var input map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if calls == 1 {
			tools := string(input["tools"])
			if !strings.Contains(tools, "search_traces") || strings.Contains(tools, "delete_everything") || !strings.Contains(tools, "read_repository_file") {
				t.Fatalf("offered tools = %s", tools)
			}
			return modelOutput(`{"output":[{"type":"function_call","call_id":"mcp-1","name":"search_traces","arguments":"{\"service\":\"marina\"}"}],"usage":{"input_tokens":10,"output_tokens":5}}`), nil
		}
		if !strings.Contains(string(input["input"]), "3 traces for marina") {
			t.Fatal("MCP tool result not delivered to the model")
		}
		return modelOutput(`{"output":[{"type":"message","content":[{"type":"output_text","text":"Three traces"}]}],"usage":{"input_tokens":30,"output_tokens":10}}`), nil
	}))
	g.MCP = &MCPTools{Endpoint: mcp.URL + "/mcp", Tools: []string{"search_traces"}}
	if status := call(`{"input":"How many traces?"}`); status != 200 {
		t.Fatalf("status=%d", status)
	}
	if calls != 2 || len(server.calls) != 1 || server.calls[0] != "search_traces" {
		t.Fatalf("model calls=%d MCP calls=%v", calls, server.calls)
	}
	for _, header := range server.headers[1:] {
		if !strings.HasSuffix(header, "|session-1") {
			t.Fatalf("MCP request without the session: %q", header)
		}
	}
}

func TestUnlistedMCPToolStopsTheTurnWithoutCallingTheServer(t *testing.T) {
	server := &fakeMCP{}
	mcp := httptest.NewTLSServer(server)
	defer mcp.Close()
	g, _, call := repositoryGateway(t, 20000, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == strings.TrimPrefix(mcp.URL, "https://") {
			return mcp.Client().Transport.RoundTrip(r)
		}
		return modelOutput(`{"output":[{"type":"function_call","call_id":"mcp-1","name":"delete_everything","arguments":"{}"}],"usage":{"input_tokens":10,"output_tokens":5}}`), nil
	}))
	g.MCP = &MCPTools{Endpoint: mcp.URL + "/mcp", Tools: []string{"search_traces"}}
	if status := call(`{"input":"Delete it"}`); status != 502 {
		t.Fatalf("status=%d", status)
	}
	if len(server.calls) != 0 {
		t.Fatalf("unlisted tool reached the server: %v", server.calls)
	}
}

func TestMCPProfileRequiresHTTPSAndASmallNamedAllowList(t *testing.T) {
	for _, tc := range []struct {
		tools MCPTools
		ok    bool
	}{
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp", Tools: []string{"search_traces"}}, true},
		{MCPTools{Endpoint: "http://agentgateway.platform-gateway.svc/mcp", Tools: []string{"search_traces"}}, false},
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp"}, false},
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp", Tools: []string{"read_repository_file"}}, false},
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp", Tools: []string{"a b"}}, false},
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp", Tools: []string{"t1", "t1"}}, false},
		{MCPTools{Endpoint: "https://agentgateway.platform-gateway.svc/mcp", Tools: []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8", "t9"}}, false},
	} {
		if err := tc.tools.Validate(); (err == nil) != tc.ok {
			t.Fatalf("%+v: err=%v", tc.tools, err)
		}
	}
}
