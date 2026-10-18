package command

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Drive a separate Marina process (the API sandbox build) only through its
// public platform API, the way an operator would (MAR-07, MAR-09).
func TestRealMarinaOperatorSession(t *testing.T) {
	binary := os.Getenv("MARINA_TEST_BINARY")
	if binary == "" {
		t.Skip("run make conformance")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	baseURL := "http://" + address
	var logs bytes.Buffer
	process := exec.Command(binary, "api-sandbox", "--addr", address, "--dir", t.TempDir())
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
		response, err := httpClient.Get(baseURL + "/apis/marina.dev/v1/capabilities")
		if response != nil {
			response.Body.Close()
		}
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Marina did not become ready")
		}
		time.Sleep(25 * time.Millisecond)
	}
	environment := func(token string) func(string) string {
		return func(name string) string {
			return map[string]string{"MARINA_URL": baseURL, "MARINA_TOKEN": token, "MARINA_NAMESPACE": "factory"}[name]
		}
	}
	invoke := func(args ...string) string {
		t.Helper()
		code, out, diagnostic := runForTest(t, args, environment("user:operator"))
		if code != 0 {
			t.Fatalf("CLI %v: %s", args, diagnostic)
		}
		return out
	}
	waitFor := func(want string, args ...string) string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			out := invoke(args...)
			if strings.Contains(out, want) {
				return out
			}
			if time.Now().After(deadline) {
				t.Fatalf("%v never showed %q: %s", args, want, out)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	create := []string{"runs", "create", "task-cli", "--project", "acceptance", "--workload", "land-on-main",
		"--title", "Reject an empty range", "--criteria", "A unit test covers it", "--minutes", "30", "--tokens", "10000", "--attempts", "7"}
	invoke(create...)
	if out := invoke(create...); !strings.Contains(out, "already exists with the same spec") {
		t.Fatalf("replay = %s", out)
	}
	waitFor("task-cli-plan-1", "decisions", "list")
	invoke("decisions", "answer", "task-cli-plan-1", "Approve plan")
	if out := invoke("runs", "events", "task-cli", "--follow"); !strings.Contains(out, "run.completed") || !strings.Contains(out, "is Succeeded") {
		t.Fatalf("follow = %s", out)
	}
	out := invoke("runs", "get", "task-cli")
	for _, want := range []string{"Phase:      Succeeded", "Landed:", "review", "verification", "landing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("run lacks %q: %s", want, out)
		}
	}

	invoke("runs", "create", "task-stop", "--project", "acceptance", "--workload", "land-on-main",
		"--title", "Stop me", "--criteria", "c", "--minutes", "30", "--tokens", "10000", "--attempts", "7")
	waitFor("WaitingDecision", "runs", "get", "task-stop")
	invoke("runs", "cancel", "task-stop")
	waitFor("Cancelled", "runs", "get", "task-stop")

	invoke("turns", "create", "turn-cli", "--project", "acceptance", "--workload", "chat", "--message", "Hello")
	waitFor("Sandbox reply to: Hello", "turns", "get", "turn-cli")

	if code, _, _ := runForTest(t, []string{"runs", "list"}, environment("")); code == 0 {
		t.Fatal("unauthenticated access accepted")
	}
}
