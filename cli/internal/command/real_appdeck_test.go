package command

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/cli/internal/appdeck"
)

// Use only the public API of a separate AppDeck process and isolated database.
func TestRealAppDeckOperatorSession(t *testing.T) {
	binary := os.Getenv("APPDECK_TEST_BINARY")
	if binary == "" {
		t.Skip("run make conformance")
	}
	directory := t.TempDir()
	token := hex.EncodeToString(randomBytes(t, 32))
	digest := sha256.Sum256([]byte(token))
	policy := fmt.Sprintf(`{"version":1,"operators":[{"id":"cli-test","token_sha256":["%x"]}]}`, digest)
	policyFile := filepath.Join(directory, "operator.json")
	tokenFile := filepath.Join(directory, "token")
	if err := os.WriteFile(policyFile, []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	baseURL := "http://" + address
	var logs bytes.Buffer
	process := exec.Command(binary)
	process.Env = append(os.Environ(), "APPDECK_ADDR="+address, "APPDECK_DATA_DIR="+filepath.Join(directory, "data"), "APPDECK_DEMO_AGENT=false", "APPDECK_OPERATOR_AUTH_FILE="+policyFile, "APPDECK_OPERATOR_COOKIE_SECURE=false", "APPDECK_SERVICE_AUTH_FILE=")
	process.Stdout = &logs
	process.Stderr = &logs
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = process.Process.Kill()
		_ = process.Wait()
		if t.Failed() {
			t.Log(logs.String())
		}
	}()
	httpClient := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := httpClient.Get(baseURL + "/readyz")
		ready := err == nil && response.StatusCode == 200
		if response != nil {
			response.Body.Close()
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("AppDeck did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	invoke := func(args ...string) string {
		t.Helper()
		options := append([]string{"--appdeck-url", baseURL, "--token-file", tokenFile, "--json"}, args...)
		code, out, diagnostic := runForTest(t, options, emptyEnvironment)
		if code != 0 {
			t.Fatalf("CLI %v: %s", args, diagnostic)
		}
		if strings.Contains(out+diagnostic, token) {
			t.Fatal("credential leaked")
		}
		return out
	}
	out := invoke("tasks", "create", "--title", "Session-authenticated work", "--acceptance", "Observable result", "--status", "ready")
	var task appdeck.Task
	if err = json.Unmarshal([]byte(out), &task); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(invoke("tasks", "list"), task.ID) {
		t.Fatal("created task not listed")
	}
	if !strings.Contains(invoke("tasks", "get", task.ID), "Session-authenticated work") {
		t.Fatal("task not inspected")
	}
	// The service boundary is independently configurable; this fixture leaves only
	// machine authentication disabled so it can establish a run for cancellation.
	post := func(path string, input any) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+path, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", path)
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		encoded, _ := io.ReadAll(response.Body)
		if response.StatusCode >= 300 {
			t.Fatalf("fixture %s: %s", path, encoded)
		}
		result := map[string]any{}
		if err = json.Unmarshal(encoded, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	post("/api/v1/agents", map[string]any{"id": "agt_cli", "installation_id": "cli-test", "name": "CLI test", "kind": "controller"})
	claimed := post("/api/v1/tasks/"+task.ID+"/claim", map[string]any{"agent_id": "agt_cli", "lease_seconds": 60, "expected_task_version": task.Version})
	runID, _ := claimed["id"].(string)
	if runID == "" {
		t.Fatalf("missing run: %v", claimed)
	}
	out = invoke("runs", "cancel", "--reason", "Operator stops work", runID)
	if !strings.Contains(out, `"type": "cancel"`) || !strings.Contains(out, `"status": "pending"`) {
		t.Fatalf("cancel not recorded: %s", out)
	}
	code, _, _ := runForTest(t, []string{"--appdeck-url", baseURL, "tasks", "list"}, emptyEnvironment)
	if code == 0 {
		t.Fatal("unauthenticated operator access accepted")
	}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
