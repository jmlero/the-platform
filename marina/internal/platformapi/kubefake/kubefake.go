// Package kubefake is an in-memory stand-in for the parts of the Kubernetes
// API the platform API uses: marina.dev custom resources (list, get, create,
// status, merge patch, delete with finalizers), TokenReview and
// SubjectAccessReview. Tests and the API sandbox use it; nothing else does.
package kubefake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	mu      sync.Mutex
	objects map[string]map[string]map[string]any
	version int
	// Users maps bearer tokens to user names for TokenReview. A token of the
	// form "user:<name>" is accepted too.
	Users map[string]string
	// Allow decides SubjectAccessReview; nil allows everything.
	Allow func(user, verb, namespace, resource, subresource string) bool
}

func New() *Server {
	return &Server{objects: map[string]map[string]map[string]any{}, Users: map[string]string{}}
}

// Put stores an object as if a client had created it.
func (s *Server) Put(plural string, object map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store(plural, object)
}

func (s *Server) store(plural string, object map[string]any) {
	meta := object["metadata"].(map[string]any)
	if meta["uid"] == nil {
		meta["uid"] = "uid-" + meta["name"].(string)
	}
	if meta["generation"] == nil {
		meta["generation"] = 1.0
	}
	if meta["creationTimestamp"] == nil {
		meta["creationTimestamp"] = time.Now().UTC().Format(time.RFC3339)
	}
	s.version++
	meta["resourceVersion"] = fmt.Sprint(s.version)
	if s.objects[plural] == nil {
		s.objects[plural] = map[string]map[string]any{}
	}
	s.objects[plural][meta["namespace"].(string)+"/"+meta["name"].(string)] = object
}

// Get returns a stored object by plural and "namespace/name".
func (s *Server) Get(plural, key string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.objects[plural][key]
}

// Edit changes a stored object under the lock, as a client update would.
func (s *Server) Edit(plural, key string, change func(map[string]any)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if object := s.objects[plural][key]; object != nil {
		change(object)
	}
}

func status(w http.ResponseWriter, code int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": reason, "message": message, "code": code})
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		if json.Unmarshal(raw, &body) != nil {
			status(w, http.StatusBadRequest, "BadRequest", "invalid JSON")
			return
		}
	}
	switch r.URL.Path {
	case "/apis/authentication.k8s.io/v1/tokenreviews":
		s.tokenReview(w, body)
		return
	case "/apis/authorization.k8s.io/v1/subjectaccessreviews":
		s.accessReview(w, body)
		return
	}
	path, ok := strings.CutPrefix(r.URL.Path, "/apis/marina.dev/v1/")
	if !ok {
		status(w, http.StatusNotFound, "NotFound", "unknown path")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := strings.Split(path, "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.list(w, parts[0], "")
		return
	}
	if len(parts) < 3 || parts[0] != "namespaces" {
		status(w, http.StatusNotFound, "NotFound", "unknown path")
		return
	}
	namespace, plural := parts[1], parts[2]
	if len(parts) == 3 {
		switch r.Method {
		case http.MethodGet:
			s.list(w, plural, namespace)
		case http.MethodPost:
			meta, _ := body["metadata"].(map[string]any)
			if meta == nil || meta["name"] == nil {
				status(w, http.StatusUnprocessableEntity, "Invalid", "metadata.name is required")
				return
			}
			meta["namespace"] = namespace
			if s.objects[plural][namespace+"/"+meta["name"].(string)] != nil {
				status(w, http.StatusConflict, "AlreadyExists", plural+" "+meta["name"].(string)+" already exists")
				return
			}
			delete(body, "status")
			s.store(plural, body)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(body)
		default:
			status(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
		}
		return
	}
	key := namespace + "/" + parts[3]
	object := s.objects[plural][key]
	if object == nil {
		status(w, http.StatusNotFound, "NotFound", plural+" "+parts[3]+" not found")
		return
	}
	meta := object["metadata"].(map[string]any)
	if rv, ok := nested(body, "metadata", "resourceVersion").(string); ok && rv != "" && rv != meta["resourceVersion"] {
		status(w, http.StatusConflict, "Conflict", "the object has been modified")
		return
	}
	switch {
	case len(parts) == 5 && parts[4] == "status" && r.Method == http.MethodPut:
		object["status"] = body["status"]
	case len(parts) == 4 && r.Method == http.MethodGet:
	case len(parts) == 4 && r.Method == http.MethodPut:
		body["status"], body["metadata"] = object["status"], meta
		if !equalJSON(body["spec"], object["spec"]) {
			meta["generation"] = meta["generation"].(float64) + 1
		}
		object = body
	case len(parts) == 4 && r.Method == http.MethodPatch:
		if spec, ok := body["spec"].(map[string]any); ok {
			before, _ := json.Marshal(object["spec"])
			for field, value := range spec {
				object["spec"].(map[string]any)[field] = value
			}
			if after, _ := json.Marshal(object["spec"]); string(after) != string(before) {
				meta["generation"] = meta["generation"].(float64) + 1
			}
		}
		if patch, ok := body["metadata"].(map[string]any); ok {
			for _, field := range []string{"finalizers", "annotations", "labels"} {
				if value, set := patch[field]; set {
					meta[field] = value
				}
			}
			if finalizers, _ := meta["finalizers"].([]any); len(finalizers) == 0 && meta["deletionTimestamp"] != nil {
				delete(s.objects[plural], key)
				_ = json.NewEncoder(w).Encode(object)
				return
			}
		}
	case len(parts) == 4 && r.Method == http.MethodDelete:
		if finalizers, _ := meta["finalizers"].([]any); len(finalizers) > 0 {
			meta["deletionTimestamp"] = time.Now().UTC().Format(time.RFC3339)
		} else {
			delete(s.objects[plural], key)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Success", "code": 200})
		return
	default:
		status(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "method not allowed")
		return
	}
	s.version++
	object["metadata"].(map[string]any)["resourceVersion"] = fmt.Sprint(s.version)
	s.objects[plural][key] = object
	_ = json.NewEncoder(w).Encode(object)
}

func (s *Server) list(w http.ResponseWriter, plural, namespace string) {
	items := []any{}
	for key, object := range s.objects[plural] {
		if namespace == "" || strings.HasPrefix(key, namespace+"/") {
			items = append(items, object)
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "marina.dev/v1", "kind": "List", "metadata": map[string]any{"resourceVersion": fmt.Sprint(s.version)}, "items": items})
}

func (s *Server) tokenReview(w http.ResponseWriter, body map[string]any) {
	token, _ := nested(body, "spec", "token").(string)
	user, ok := s.Users[token]
	if name, prefixed := strings.CutPrefix(token, "user:"); !ok && prefixed && name != "" {
		user, ok = name, true
	}
	result := map[string]any{"authenticated": false}
	if ok {
		result = map[string]any{"authenticated": true, "user": map[string]any{"username": user, "uid": "uid-" + user, "groups": []string{"system:authenticated"}}}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": result})
}

func (s *Server) accessReview(w http.ResponseWriter, body map[string]any) {
	user, _ := nested(body, "spec", "user").(string)
	attributes, _ := nested(body, "spec", "resourceAttributes").(map[string]any)
	field := func(name string) string { value, _ := attributes[name].(string); return value }
	allowed := s.Allow == nil || s.Allow(user, field("verb"), field("namespace"), field("resource"), field("subresource"))
	_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"allowed": allowed}})
}

func nested(value any, path ...string) any {
	for _, key := range path {
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = m[key]
	}
	return value
}

func equalJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
