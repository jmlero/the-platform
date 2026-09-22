package workerapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Run in an isolated, network-none container with the Codex 0.148.0 binary from
// the locked kagent image. This proves the native request contract, not Substrate
// credential injection or live kagent deployment. No real model is contacted.
func TestNativeCodexGateway(t *testing.T) {
	binary := os.Getenv("KINT04_CODEX_BINARY")
	if binary == "" {
		t.Skip("set KINT04_CODEX_BINARY to the isolated pinned native binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.148.0" {
		t.Fatal("native binary differs from kagent lock")
	}
	s, a, _, _ := engineFixture(t, 1000000)
	token, err := s.EngineCredential(ctx, a.Specification)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if json.NewDecoder(r.Body).Decode(&request) != nil || !localTools(request["tools"]) {
			t.Error("native provider tools escaped normalization")
		}
		if _, ok := request["client_metadata"]; ok {
			t.Error("native metadata reached provider")
		}
		w.Header().Set("Content-Type", "text/event-stream")

		response := map[string]any{"id": "resp_fixture", "object": "response", "status": "in_progress", "output": []any{}}
		item := map[string]any{"id": "msg_fixture", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}
		part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
		seq := 0
		event := func(kind string, fields map[string]any) {
			fields["type"] = kind
			fields["sequence_number"] = seq
			seq++
			data, _ := json.Marshal(fields)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
		}
		event("response.created", map[string]any{"response": response})
		event("response.output_item.added", map[string]any{"output_index": 0, "item": item})
		event("response.content_part.added", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": part})
		event("response.output_text.delta", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": "fixture complete"})
		part["text"] = "fixture complete"
		event("response.output_text.done", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "text": "fixture complete"})
		event("response.content_part.done", map[string]any{"item_id": "msg_fixture", "output_index": 0, "content_index": 0, "part": part})
		item["status"] = "completed"
		item["content"] = []any{part}
		event("response.output_item.done", map[string]any{"output_index": 0, "item": item})
		response["status"] = "completed"
		response["output"] = []any{item}
		response["usage"] = map[string]any{"input_tokens": 2, "output_tokens": 1, "total_tokens": 3}
		event("response.completed", map[string]any{"response": response})

	}))
	defer upstream.Close()
	s.client = upstream.Client()
	s.config.Model = Model{Endpoint: upstream.URL, Name: "gpt-5", MaxOutputTokens: 100}
	received := make(chan map[string]any, 10)
	gateway, rootPEM := nativeTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var value map[string]any
		if json.Unmarshal(body, &value) == nil {
			select {
			case received <- value:
			default:
			}
		}
		s.ServeHTTP(w, r)
	}))
	defer gateway.Close()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(ca, rootPEM, 0600); err != nil {
		t.Fatal(err)
	}
	// Match the pinned native translator's relevant config, including cached
	// search. No agent templates, memory, MCP, skills, plugins or telemetry.
	config := fmt.Sprintf("model = \"gpt-5\"\nmodel_provider = \"kagent-openai\"\nsandbox_mode = \"danger-full-access\"\nweb_search = \"cached\"\n[approval_policy.granular]\nsandbox_approval=false\nrules=false\nskill_approval=false\nrequest_permissions=false\nmcp_elicitations=true\n[features]\ndefault_mode_request_user_input=true\n[analytics]\nenabled=false\n[model_providers.kagent-openai]\nname=\"OpenAI\"\nwire_api=\"responses\"\nenv_key=\"OPENAI_API_KEY\"\nbase_url=%q\n", gateway.URL+"/engine/model/v1")
	if err = os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--strict-config", "--stdio")
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "CODEX_HOME=" + dir, "OPENAI_API_KEY=" + token, "SSL_CERT_FILE=" + ca, "CODEX_CA_CERTIFICATE=" + ca}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Stderr may contain request diagnostics. Keep it out of evidence/logs.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); cmd.Process.Kill(); cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	encoder := json.NewEncoder(stdin)
	call := func(id int, method string, params any) map[string]any {
		t.Helper()
		if err := encoder.Encode(map[string]any{"id": id, "method": method, "params": params}); err != nil {
			t.Fatal(err)
		}
		for scanner.Scan() {
			var msg map[string]any
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				t.Fatal("invalid native JSON-RPC")
			}
			if msg["id"] == float64(id) {
				if msg["error"] != nil {
					t.Fatal("native JSON-RPC rejected config")
				}
				return msg["result"].(map[string]any)
			}
		}
		t.Fatal("native JSON-RPC ended")
		return nil
	}
	call(1, "initialize", map[string]any{"clientInfo": map[string]string{"name": "kagent-codex", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if err = encoder.Encode(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	thread := call(2, "thread/start", map[string]any{"cwd": dir, "model": "gpt-5", "modelProvider": "kagent-openai", "approvalPolicy": map[string]any{"granular": map[string]bool{"sandbox_approval": false, "rules": false, "skill_approval": false, "request_permissions": false, "mcp_elicitations": true}}, "sandbox": "danger-full-access", "developerInstructions": "Synthetic protocol fixture. Reply briefly without using tools."})
	id := thread["thread"].(map[string]any)["id"].(string)
	call(3, "turn/start", map[string]any{"threadId": id, "input": []map[string]any{{"type": "text", "text": "Return fixture complete."}}})
	for scanner.Scan() {
		var msg map[string]any
		json.Unmarshal(scanner.Bytes(), &msg)
		if msg["method"] == "error" {
			b, _ := json.Marshal(msg["params"].(map[string]any)["error"])
			t.Log(strings.ReplaceAll(string(b), token, "[redacted]"))
		}
		if msg["method"] == "turn/completed" {
			if msg["params"].(map[string]any)["turn"].(map[string]any)["status"] != "completed" {
				t.Error("native turn failed")
			}
			break
		}
	}
	select {
	case request := <-received:
		var keys, tools []string
		for key := range request {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, tool := range request["tools"].([]any) {
			m := tool.(map[string]any)
			kind, _ := m["type"].(string)
			if kind == "web_search" {
				b, _ := json.Marshal(m)
				tools = append(tools, string(b))
			} else {
				tools = append(tools, kind)
			}
		}
		t.Logf("native request fields: %v; tool kinds: %v", keys, tools)
	default:
		t.Fatal("native request never reached TLS gateway")
	}
	totals, err := s.journal.ExecutionModelTotals(context.Background(), a.ExecutionID)
	if err != nil || totals.Tokens != 3 || totals.PendingCalls != 0 {
		t.Fatalf("native model call not admitted/accounted: %+v %v", totals, err)
	}
}

func nativeTLSServer(t *testing.T, handler http.Handler) (*httptest.Server, []byte) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "KINT-04 ephemeral test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, BasicConstraintsValid: true, IsCA: true}
	rootDER, err := x509.CreateCertificate(rand.Reader, root, root, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: root.NotBefore, NotAfter: root.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, leaf, root, &key.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der, rootDER}, PrivateKey: key}}}
	server.StartTLS()
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})
}
