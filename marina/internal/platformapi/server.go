package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubeapi"
)

const maxBody = 3 << 19 // 1.5 MiB, the practical limit of one stored resource

var kinds = map[string]string{"projects": "Project", "workloads": "Workload", "runs": "Run", "turns": "Turn", "decisions": "Decision"}

// Server is the HTTP/JSON surface of marina.dev/v1. Writes become the same
// custom resources kubectl creates; reads, events and artifacts come from the
// resources and Marina's ledger.
type Server struct {
	Kube         *kubeapi.Client
	Platform     *sqlite.Platform
	Artifacts    *artifacts.Store
	Capabilities func(context.Context) (Capabilities, error)
	Now          func() time.Time
	// Poll is how often an event stream checks for new events.
	Poll time.Duration
}

// Handler serves the API behind Kubernetes authentication and authorization.
func (s *Server) Handler(auth *Authorizer) http.Handler {
	return auth.Require(http.HandlerFunc(s.serve))
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	a, ok := attributesFor(r.Method, r.URL.Path)
	if !ok {
		writeStatus(w, http.StatusNotFound, "NotFound", "not part of the marina.dev/v1 API")
		return
	}
	if a.Resource == "capabilities" {
		capabilities, err := s.Capabilities(r.Context())
		if err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "capabilities unavailable")
			return
		}
		capabilities.APIVersion, capabilities.Kind = apiVersion, "Capabilities"
		writeJSON(w, http.StatusOK, capabilities)
		return
	}
	collection := "/apis/" + apiVersion + "/namespaces/" + a.Namespace + "/" + a.Resource
	switch {
	case a.Subresource == "events":
		s.events(w, r, a, collection)
	case a.Subresource == "artifacts":
		s.artifact(w, r, a, collection, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
	case a.Subresource == "cancel":
		s.cancel(w, r, a, collection)
	case a.Subresource == "answer":
		s.answer(w, r, a, collection)
	case a.Verb == "list":
		s.proxy(w, r, http.MethodGet, collection, nil)
	case a.Verb == "get":
		s.proxy(w, r, http.MethodGet, collection+"/"+a.Name, nil)
	case a.Verb == "delete":
		s.proxy(w, r, http.MethodDelete, collection+"/"+a.Name, nil)
	case a.Verb == "create":
		s.create(w, r, a, collection)
	case a.Verb == "update":
		if a.Resource != "projects" && a.Resource != "workloads" {
			writeStatus(w, http.StatusMethodNotAllowed, "Invalid", "runs, turns and decisions are not replaced; cancel or answer them")
			return
		}
		object, ok := readObject(w, r, a)
		if ok {
			s.proxy(w, r, http.MethodPut, collection+"/"+a.Name, object)
		}
	default:
		writeStatus(w, http.StatusMethodNotAllowed, "Invalid", "method not allowed")
	}
}

// readObject decodes a request body as the resource's object and pins its
// kind, API version and namespace.
func readObject(w http.ResponseWriter, r *http.Request, a Attributes) (map[string]any, bool) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeStatus(w, http.StatusRequestEntityTooLarge, "Invalid", "the request body exceeds 1.5 MiB")
		return nil, false
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil || object == nil {
		writeStatus(w, http.StatusBadRequest, "Invalid", "the body must be a JSON object")
		return nil, false
	}
	meta, _ := object["metadata"].(map[string]any)
	if object["apiVersion"] != apiVersion || object["kind"] != kinds[a.Resource] || meta == nil {
		writeStatus(w, http.StatusBadRequest, "Invalid", "apiVersion must be "+apiVersion+" and kind "+kinds[a.Resource])
		return nil, false
	}
	if ns, set := meta["namespace"]; set && ns != a.Namespace {
		writeStatus(w, http.StatusBadRequest, "Invalid", "metadata.namespace does not match the path")
		return nil, false
	}
	if a.Name != "" && meta["name"] != a.Name {
		writeStatus(w, http.StatusBadRequest, "Invalid", "metadata.name does not match the path")
		return nil, false
	}
	meta["namespace"] = a.Namespace
	delete(object, "status")
	for _, field := range []string{"uid", "resourceVersion", "generation", "creationTimestamp", "finalizers", "deletionTimestamp"} {
		if field != "resourceVersion" || a.Verb == "create" {
			delete(meta, field)
		}
	}
	return object, true
}

// create makes the resource, with the caller recorded as its creator. The
// name is the idempotency key: the same spec returns the existing object.
func (s *Server) create(w http.ResponseWriter, r *http.Request, a Attributes, collection string) {
	if a.Resource == "decisions" {
		writeStatus(w, http.StatusMethodNotAllowed, "Invalid", "Marina creates decisions; answer them instead")
		return
	}
	object, ok := readObject(w, r, a)
	if !ok {
		return
	}
	meta := object["metadata"].(map[string]any)
	name, _ := meta["name"].(string)
	if !dnsLabel.MatchString(name) || len(name) > 63 {
		writeStatus(w, http.StatusUnprocessableEntity, "Invalid", "metadata.name must be a DNS label")
		return
	}
	annotations, _ := meta["annotations"].(map[string]any)
	if annotations == nil {
		annotations = map[string]any{}
	}
	annotations[creatorAnnotation] = UserFrom(r.Context()).Name
	meta["annotations"] = annotations
	var created map[string]any
	err := s.Kube.Do(r.Context(), http.MethodPost, collection, "", object, &created)
	if kubeapi.IsStatus(err, http.StatusConflict) {
		var existing map[string]any
		if getErr := s.Kube.Do(r.Context(), http.MethodGet, collection+"/"+name, "", nil, &existing); getErr != nil {
			writeKubeError(w, getErr)
			return
		}
		if sameJSON(existing["spec"], object["spec"]) {
			writeJSON(w, http.StatusOK, existing)
			return
		}
		writeStatus(w, http.StatusConflict, "AlreadyExists", fmt.Sprintf("%s.%s %q already exists with a different spec", a.Resource, group, name))
		return
	}
	if err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func sameJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	c, _ := json.Marshal(x)
	d, _ := json.Marshal(y)
	return bytes.Equal(c, d)
}

func (s *Server) proxy(w http.ResponseWriter, r *http.Request, method, path string, body any) {
	var result map[string]any
	if err := s.Kube.Do(r.Context(), method, path, "", body, &result); err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) bool {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 200 {
		writeStatus(w, http.StatusBadRequest, "Invalid", "an Idempotency-Key header is required")
		return false
	}
	return true
}

// cancel sets spec.cancel; repeating it changes nothing.
func (s *Server) cancel(w http.ResponseWriter, r *http.Request, a Attributes, collection string) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	patch := map[string]any{"spec": map[string]any{"cancel": true}}
	var result map[string]any
	if err := s.Kube.Do(r.Context(), http.MethodPatch, collection+"/"+a.Name, "application/merge-patch+json", patch, &result); err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// answer records the caller's answer once. An exact replay returns the
// Decision; a different answer, or one after expiry, is a conflict.
func (s *Server) answer(w http.ResponseWriter, r *http.Request, a Attributes, collection string) {
	if !requireIdempotencyKey(w, r) {
		return
	}
	var body DecisionAnswer
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body.Answer) == "" {
		writeStatus(w, http.StatusBadRequest, "Invalid", `the body must be {"answer": "..."}`)
		return
	}
	var decision Decision
	if err := s.Kube.Do(r.Context(), http.MethodGet, collection+"/"+a.Name, "", nil, &decision); err != nil {
		writeKubeError(w, err)
		return
	}
	if response := decision.Spec.Response; response != nil {
		if strings.EqualFold(response.Answer, body.Answer) {
			writeJSON(w, http.StatusOK, decision)
			return
		}
		writeStatus(w, http.StatusConflict, "Conflict", "the decision already has a different answer")
		return
	}
	if decision.Spec.ExpiresAt != "" {
		if expires, err := time.Parse(time.RFC3339, decision.Spec.ExpiresAt); err == nil && !s.now().Before(expires) {
			writeStatus(w, http.StatusConflict, "Conflict", "the decision has expired")
			return
		}
	}
	answer := body.Answer
	if len(decision.Spec.Options) > 0 {
		matched := false
		for _, option := range decision.Spec.Options {
			if strings.EqualFold(option, body.Answer) {
				answer, matched = option, true
			}
		}
		if !matched {
			writeStatus(w, http.StatusUnprocessableEntity, "Invalid", "the answer must be one of: "+strings.Join(decision.Spec.Options, ", "))
			return
		}
	}
	patch := map[string]any{"metadata": map[string]any{"resourceVersion": decision.Metadata.ResourceVersion},
		"spec": map[string]any{"response": DecisionResponse{Answer: answer, AnsweredBy: UserFrom(r.Context()).Name, AnsweredAt: rfc3339(s.now())}}}
	var result map[string]any
	if err := s.Kube.Do(r.Context(), http.MethodPatch, collection+"/"+a.Name, "application/merge-patch+json", patch, &result); err != nil {
		writeKubeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func ownerID(a Attributes) string { return sqlite.PlatformWorkID(a.Namespace, a.Name) }

// events returns ordered events after a sequence, or streams them as
// server-sent events until the client disconnects.
func (s *Server) events(w http.ResponseWriter, r *http.Request, a Attributes, collection string) {
	if err := s.Kube.Do(r.Context(), http.MethodGet, collection+"/"+a.Name, "", nil, nil); err != nil {
		writeKubeError(w, err)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if after < 0 {
		after = 0
	}
	page := func() (EventPage, error) {
		events, err := s.Platform.ListEvents(r.Context(), ownerID(a), after, 100)
		out := EventPage{Items: []Event{}}
		for _, e := range events {
			out.Items = append(out.Items, Event{Sequence: e.Sequence, Type: e.Type, Time: rfc3339(e.OccurredAt), Message: e.Message, Fields: e.Fields})
			out.NextAfter = e.Sequence
		}
		if out.NextAfter == 0 {
			out.NextAfter = after
		}
		return out, err
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		events, err := page()
		if err != nil {
			writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "events unavailable")
			return
		}
		writeJSON(w, http.StatusOK, events)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatus(w, http.StatusNotAcceptable, "Invalid", "streaming is not available")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	poll := s.Poll
	if poll <= 0 {
		poll = time.Second
	}
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		events, err := page()
		if err != nil {
			return
		}
		for _, e := range events.Items {
			data, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.Sequence, e.Type, data); err != nil {
				return
			}
		}
		after = events.NextAfter
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

// artifact serves a retained evidence file of this Run only.
func (s *Server) artifact(w http.ResponseWriter, r *http.Request, a Attributes, collection, digest string) {
	if err := s.Kube.Do(r.Context(), http.MethodGet, collection+"/"+a.Name, "", nil, nil); err != nil {
		writeKubeError(w, err)
		return
	}
	evidence, err := s.Platform.Evidence(r.Context(), ownerID(a))
	if err != nil {
		writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "evidence unavailable")
		return
	}
	owned := false
	for _, item := range evidence {
		owned = owned || item.ArtifactDigest == digest
	}
	if !owned || s.Artifacts == nil {
		writeStatus(w, http.StatusNotFound, "NotFound", "this run has no artifact "+digest)
		return
	}
	data, err := s.Artifacts.Get(digest)
	if err != nil {
		writeStatus(w, http.StatusGone, "Gone", "the artifact is no longer retained")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

// writeKubeError passes a Kubernetes rejection on as the same Status.
func writeKubeError(w http.ResponseWriter, err error) {
	var status *kubeapi.StatusError
	if errors.As(err, &status) {
		reason := status.Reason
		if reason == "" {
			reason = http.StatusText(status.Code)
		}
		writeStatus(w, status.Code, reason, status.Message)
		return
	}
	writeStatus(w, http.StatusServiceUnavailable, "ServiceUnavailable", "the Kubernetes API is unavailable")
}
