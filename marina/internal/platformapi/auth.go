// Package platformapi serves Marina's platform API v1 over HTTP/JSON
// (marina/api/openapi.yaml). Every request carries a Kubernetes token and is
// authorized with the same RBAC as the marina.dev custom resources.
package platformapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jmlero/the-platform/marina/internal/kubeapi"
)

const (
	group    = "marina.dev"
	version  = "v1"
	prefix   = "/apis/" + group + "/" + version
	cacheTTL = 30 * time.Second
)

// Attributes are the Kubernetes authorization attributes of one request.
type Attributes struct {
	Verb, Namespace, Resource, Subresource, Name string
}

type User struct {
	Name, UID string
	Groups    []string
}

var (
	dnsLabel  = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	digestRef = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// The subresources each resource offers, with the verb each method maps to.
	subresources = map[string]map[string]map[string]string{
		"runs":      {"cancel": {"POST": "update"}, "events": {"GET": "get"}, "artifacts": {"GET": "get"}},
		"turns":     {"cancel": {"POST": "update"}, "events": {"GET": "get"}},
		"decisions": {"answer": {"POST": "update"}},
		"projects":  {},
		"workloads": {},
	}
)

// attributesFor maps a request to authorization attributes, or reports that
// the path is not part of the API.
func attributesFor(method, path string) (Attributes, bool) {
	if path == prefix+"/capabilities" {
		return Attributes{Verb: "get", Resource: "capabilities"}, method == http.MethodGet
	}
	rest, ok := strings.CutPrefix(path, prefix+"/namespaces/")
	if !ok {
		return Attributes{}, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || !dnsLabel.MatchString(parts[0]) || len(parts[0]) > 63 {
		return Attributes{}, false
	}
	a := Attributes{Namespace: parts[0], Resource: parts[1]}
	subs, known := subresources[a.Resource]
	if !known {
		return Attributes{}, false
	}
	switch len(parts) {
	case 2:
		a.Verb = map[string]string{"GET": "list", "POST": "create"}[method]
	case 3:
		a.Name = parts[2]
		a.Verb = map[string]string{"GET": "get", "PUT": "update", "DELETE": "delete"}[method]
	case 4, 5:
		a.Name, a.Subresource = parts[2], parts[3]
		verbs, ok := subs[a.Subresource]
		if !ok || (len(parts) == 5) != (a.Subresource == "artifacts") || (len(parts) == 5 && !digestRef.MatchString(parts[4])) {
			return Attributes{}, false
		}
		a.Verb = verbs[method]
	default:
		return Attributes{}, false
	}
	if a.Name != "" && (!dnsLabel.MatchString(a.Name) || len(a.Name) > 63) {
		return Attributes{}, false
	}
	return a, a.Verb != ""
}

type cached struct {
	user    User
	allowed bool
	expires time.Time
}

// Authorizer authenticates with TokenReview and authorizes with
// SubjectAccessReview. Results are cached briefly by token digest.
type Authorizer struct {
	kube  *kubeapi.Client
	mu    sync.Mutex
	users map[[32]byte]cached
	grant map[string]cached
	now   func() time.Time
}

func NewAuthorizer(kube *kubeapi.Client) *Authorizer {
	return &Authorizer{kube: kube, users: map[[32]byte]cached{}, grant: map[string]cached{}, now: time.Now}
}

type userKey struct{}

// UserFrom returns the authenticated caller of a request.
func UserFrom(ctx context.Context) User {
	user, _ := ctx.Value(userKey{}).(User)
	return user
}

func (a *Authorizer) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attributes, ok := attributesFor(r.Method, r.URL.Path)
		if !ok {
			writeStatus(w, http.StatusNotFound, "NotFound", "not part of the marina.dev/v1 API")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			writeStatus(w, http.StatusUnauthorized, "Unauthorized", "a Kubernetes bearer token is required")
			return
		}
		user, ok, err := a.authenticate(r.Context(), token)
		if err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "identity review unavailable")
			return
		}
		if !ok {
			writeStatus(w, http.StatusUnauthorized, "Unauthorized", "the token is not valid")
			return
		}
		allowed, err := a.authorize(r.Context(), user, attributes)
		if err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "access review unavailable")
			return
		}
		if !allowed {
			writeStatus(w, http.StatusForbidden, "Forbidden", user.Name+" cannot "+attributes.Verb+" "+attributes.Resource+" in "+attributes.Namespace)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

func (a *Authorizer) authenticate(ctx context.Context, token string) (User, bool, error) {
	key := sha256.Sum256([]byte(token))
	a.mu.Lock()
	entry, hit := a.users[key]
	a.mu.Unlock()
	if hit && a.now().Before(entry.expires) {
		return entry.user, entry.allowed, nil
	}
	var review struct {
		Status struct {
			Authenticated bool `json:"authenticated"`
			User          struct {
				Username string   `json:"username"`
				UID      string   `json:"uid"`
				Groups   []string `json:"groups"`
			} `json:"user"`
		} `json:"status"`
	}
	body := map[string]any{"apiVersion": "authentication.k8s.io/v1", "kind": "TokenReview", "spec": map[string]any{"token": token}}
	if err := a.kube.Do(ctx, http.MethodPost, "/apis/authentication.k8s.io/v1/tokenreviews", "", body, &review); err != nil {
		return User{}, false, err
	}
	user := User{Name: review.Status.User.Username, UID: review.Status.User.UID, Groups: review.Status.User.Groups}
	ok := review.Status.Authenticated && user.Name != ""
	a.mu.Lock()
	a.users[key] = cached{user: user, allowed: ok, expires: a.now().Add(cacheTTL)}
	a.mu.Unlock()
	return user, ok, nil
}

func (a *Authorizer) authorize(ctx context.Context, user User, attributes Attributes) (bool, error) {
	encoded, _ := json.Marshal([]any{user, attributes})
	key := string(encoded)
	a.mu.Lock()
	entry, hit := a.grant[key]
	a.mu.Unlock()
	if hit && a.now().Before(entry.expires) {
		return entry.allowed, nil
	}
	resource := map[string]any{"group": group, "version": version, "resource": attributes.Resource, "verb": attributes.Verb}
	for field, value := range map[string]string{"namespace": attributes.Namespace, "subresource": attributes.Subresource, "name": attributes.Name} {
		if value != "" {
			resource[field] = value
		}
	}
	body := map[string]any{"apiVersion": "authorization.k8s.io/v1", "kind": "SubjectAccessReview",
		"spec": map[string]any{"user": user.Name, "uid": user.UID, "groups": user.Groups, "resourceAttributes": resource}}
	var review struct {
		Status struct {
			Allowed bool `json:"allowed"`
		} `json:"status"`
	}
	if err := a.kube.Do(ctx, http.MethodPost, "/apis/authorization.k8s.io/v1/subjectaccessreviews", "", body, &review); err != nil {
		return false, err
	}
	a.mu.Lock()
	a.grant[key] = cached{allowed: review.Status.Allowed, expires: a.now().Add(cacheTTL)}
	a.mu.Unlock()
	return review.Status.Allowed, nil
}

// writeStatus answers with a Kubernetes Status object.
func writeStatus(w http.ResponseWriter, code int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	status := "Failure"
	if code < 400 {
		status = "Success"
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": status, "reason": reason, "message": message, "code": code})
}
