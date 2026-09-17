package appdeck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginFailureDoesNotExposeCredentialsOrDispatch(t *testing.T) {
	const token = "secret-operator-token"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/operator/session" {
			t.Error("dispatched after failed login")
		}
		writeTestJSON(t, w, 401, map[string]string{"message": token})
	}))
	defer server.Close()
	client, err := New(server.URL, token, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateTask(context.Background(), CreateTaskInput{Title: "test"})
	if err == nil || strings.Contains(err.Error(), token) || calls != 1 {
		t.Fatalf("unsafe login failure: calls=%d error=%v", calls, err)
	}
}

func TestLoginRefusesRedirectAndMissingCookies(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusCreated} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", "/forward-token")
				w.WriteHeader(status)
			}))
			defer server.Close()
			client, err := New(server.URL, "secret", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = client.Bootstrap(context.Background()); err == nil {
				t.Fatal("invalid login accepted")
			}
			if calls != 1 {
				t.Fatal("forwarded credentials or dispatched without session")
			}
		})
	}
}

func TestExpiredSessionDoesNotReplayMutation(t *testing.T) {
	logins, mutations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/operator/session" {
			logins++
			http.SetCookie(w, &http.Cookie{Name: "appdeck_operator_session", Value: "session", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "appdeck_operator_csrf", Value: "csrf", Path: "/"})
			w.WriteHeader(201)
			return
		}
		mutations++
		w.WriteHeader(401)
	}))
	defer server.Close()
	client, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.CreateTask(context.Background(), CreateTaskInput{Title: "test"}); err == nil {
		t.Fatal("expired session accepted")
	}
	if logins != 1 || mutations != 1 {
		t.Fatalf("request replayed: logins=%d mutations=%d", logins, mutations)
	}
}
