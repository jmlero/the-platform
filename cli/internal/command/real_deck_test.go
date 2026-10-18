package command

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jmlero/the-platform/cli/internal/deck"
)

// Use only the public operator API of a separate Deck process and isolated
// database. Deck runs without Marina here.
func TestRealDeckOperatorSession(t *testing.T) {
	binary := os.Getenv("DECK_TEST_BINARY")
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
	process.Env = append(os.Environ(), "DECK_ADDR="+address, "DECK_DATA_DIR="+filepath.Join(directory, "data"), "DECK_OPERATOR_AUTH_FILE="+policyFile, "DECK_OPERATOR_COOKIE_SECURE=false")
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
			t.Fatal("Deck did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	invoke := func(args ...string) string {
		t.Helper()
		options := append([]string{"--deck-url", baseURL, "--token-file", tokenFile, "--json"}, args...)
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
	var task deck.Task
	if err = json.Unmarshal([]byte(out), &task); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(invoke("tasks", "list"), task.ID) {
		t.Fatal("created task not listed")
	}
	if !strings.Contains(invoke("tasks", "get", task.ID), "Session-authenticated work") {
		t.Fatal("task not inspected")
	}
	// Run control goes through Marina; a run Deck does not know is refused.
	if code, _, _ := runForTest(t, []string{"--deck-url", baseURL, "--token-file", tokenFile, "runs", "cancel", "--reason", "Operator stops work", "run-unknown"}, emptyEnvironment); code == 0 {
		t.Fatal("cancel of an unknown run accepted")
	}
	code, _, _ := runForTest(t, []string{"--deck-url", baseURL, "tasks", "list"}, emptyEnvironment)
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
