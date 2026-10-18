package platformapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/kubeapi"
)

// fakeReviews answers TokenReview for "deck-token" and allows the deck
// service account to create runs in factory-default only.
func fakeReviews(t *testing.T, reviews *atomic.Int32) *kubeapi.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reviews.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		spec := body["spec"].(map[string]any)
		switch r.URL.Path {
		case "/apis/authentication.k8s.io/v1/tokenreviews":
			status := map[string]any{"authenticated": false}
			if spec["token"] == "deck-token" {
				status = map[string]any{"authenticated": true, "user": map[string]any{"username": "system:serviceaccount:marina-system:deck", "uid": "u1", "groups": []string{"system:serviceaccounts"}}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": status})
		case "/apis/authorization.k8s.io/v1/subjectaccessreviews":
			attributes := spec["resourceAttributes"].(map[string]any)
			allowed := spec["user"] == "system:serviceaccount:marina-system:deck" && attributes["group"] == "marina.dev" &&
				attributes["namespace"] == "factory-default" && attributes["resource"] == "runs" && (attributes["verb"] == "create" || attributes["verb"] == "get")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"allowed": allowed}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := kubeapi.New(kubeapi.Config{ServerURL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestRequestsAreAuthorizedByKubernetesIdentityAndRBAC(t *testing.T) {
	var reviews atomic.Int32
	reached := 0
	auth := NewAuthorizer(fakeReviews(t, &reviews))
	handler := auth.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached++
		if user := UserFrom(r.Context()); user.Name != "system:serviceaccount:marina-system:deck" {
			t.Errorf("user = %+v", user)
		}
	}))
	for _, tc := range []struct {
		name, method, path, token string
		want                      int
	}{
		{"no token", "POST", "/apis/marina.dev/v1/namespaces/factory-default/runs", "", 401},
		{"unknown token", "POST", "/apis/marina.dev/v1/namespaces/factory-default/runs", "stolen", 401},
		{"allowed create", "POST", "/apis/marina.dev/v1/namespaces/factory-default/runs", "deck-token", 200},
		{"allowed get", "GET", "/apis/marina.dev/v1/namespaces/factory-default/runs/task-1", "deck-token", 200},
		{"list is a different verb", "GET", "/apis/marina.dev/v1/namespaces/factory-default/runs", "deck-token", 403},
		{"other namespace", "POST", "/apis/marina.dev/v1/namespaces/other/runs", "deck-token", 403},
		{"other resource", "POST", "/apis/marina.dev/v1/namespaces/factory-default/workloads", "deck-token", 403},
		{"cancel needs update on the cancel subresource", "POST", "/apis/marina.dev/v1/namespaces/factory-default/runs/task-1/cancel", "deck-token", 403},
		{"unknown path", "GET", "/apis/marina.dev/v1/namespaces/factory-default/secrets", "deck-token", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want >= 400 && !strings.Contains(w.Body.String(), `"kind":"Status"`) {
				t.Fatalf("error is not a Kubernetes Status: %s", w.Body.String())
			}
		})
	}
	if reached != 2 {
		t.Fatalf("handler reached %d times, want 2", reached)
	}
	before := reviews.Load()
	r := httptest.NewRequest("GET", "/apis/marina.dev/v1/namespaces/factory-default/runs/task-1", nil)
	r.Header.Set("Authorization", "Bearer deck-token")
	handler.ServeHTTP(httptest.NewRecorder(), r)
	if reviews.Load() != before {
		t.Fatalf("a repeated request was reviewed again (%d reviews)", reviews.Load()-before)
	}
}

func TestRequestAttributesFollowKubernetesVerbs(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         Attributes
	}{
		{"GET", "/apis/marina.dev/v1/capabilities", Attributes{Verb: "get", Resource: "capabilities"}},
		{"GET", "/apis/marina.dev/v1/namespaces/ns/turns", Attributes{Verb: "list", Namespace: "ns", Resource: "turns"}},
		{"PUT", "/apis/marina.dev/v1/namespaces/ns/projects/p", Attributes{Verb: "update", Namespace: "ns", Resource: "projects", Name: "p"}},
		{"DELETE", "/apis/marina.dev/v1/namespaces/ns/runs/r", Attributes{Verb: "delete", Namespace: "ns", Resource: "runs", Name: "r"}},
		{"GET", "/apis/marina.dev/v1/namespaces/ns/runs/r/events", Attributes{Verb: "get", Namespace: "ns", Resource: "runs", Subresource: "events", Name: "r"}},
		{"GET", "/apis/marina.dev/v1/namespaces/ns/runs/r/artifacts/sha256:" + strings.Repeat("a", 64), Attributes{Verb: "get", Namespace: "ns", Resource: "runs", Subresource: "artifacts", Name: "r"}},
		{"POST", "/apis/marina.dev/v1/namespaces/ns/decisions/d/answer", Attributes{Verb: "update", Namespace: "ns", Resource: "decisions", Subresource: "answer", Name: "d"}},
	} {
		got, ok := attributesFor(tc.method, tc.path)
		if !ok || got != tc.want {
			t.Errorf("%s %s = %+v, %v; want %+v", tc.method, tc.path, got, ok, tc.want)
		}
	}
	for _, path := range []string{"/apis/marina.dev/v1/namespaces/NS/runs", "/apis/marina.dev/v2/capabilities", "/apis/marina.dev/v1/namespaces/ns/runs/r/logs", "/apis/marina.dev/v1/namespaces/ns/secrets/s"} {
		if _, ok := attributesFor("GET", path); ok {
			t.Errorf("%s accepted", path)
		}
	}
}
