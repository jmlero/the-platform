package marina

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMarina serves the marina.dev/v1 paths Deck uses from in-memory objects
// that a test advances directly. Created objects keep their exact spec, so a
// replayed create is accepted and a changed one is AlreadyExists.
type fakeMarina struct {
	t         *testing.T
	mu        sync.Mutex
	runs      map[string]*fakeRun
	turns     map[string]*fakeTurn
	decisions map[string]*Decision
	answers   []string
	cancels   []string
	creates   int
	// loseCreates stores the next created objects but answers 503, like a
	// response lost after Marina committed.
	loseCreates  int
	rejectCreate string
	// refuseCreate answers creates with this status code and stores nothing.
	refuseCreate int
	// projectDown answers the Project with 503; plainNotFound answers every
	// request with a bare 404, like a wrong URL or a Marina without this API.
	projectDown   bool
	plainNotFound bool
	project       []byte
	workloads     map[string][]byte
}

type fakeRun struct {
	Spec   json.RawMessage
	Run    Run
	Events []Event
}

type fakeTurn struct {
	Spec TurnSpec
	Turn Turn
}

func newFakeMarina(t *testing.T) (*fakeMarina, *httptest.Server) {
	f := &fakeMarina{t: t, runs: map[string]*fakeRun{}, turns: map[string]*fakeTurn{}, decisions: map[string]*Decision{},
		project: fixture(t, "project.json"), workloads: map[string][]byte{"land-on-main": fixture(t, "workload-repository.json"), "chat": fixture(t, "workload-conversation.json")}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeMarina) status(w http.ResponseWriter, code int, reason, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "v1", "kind": "Status", "status": "Failure", "reason": reason, "message": message, "code": code})
}

func (f *fakeMarina) write(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if raw, ok := value.([]byte); ok {
		_, _ = w.Write(raw)
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func (f *fakeMarina) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer deck-token" {
		f.status(w, 401, "Unauthorized", "token required")
		return
	}
	if f.plainNotFound {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, apiPrefix)
	if path == "/capabilities" {
		f.write(w, 200, fixture(f.t, "capabilities.json"))
		return
	}
	parts := strings.Split(strings.TrimPrefix(path, "/namespaces/factory/"), "/")
	body, _ := io.ReadAll(r.Body)
	switch {
	case parts[0] == "projects" && len(parts) == 2 && f.projectDown:
		f.status(w, 503, "ServiceUnavailable", "project store unavailable")
	case parts[0] == "projects" && len(parts) == 2:
		f.write(w, 200, f.project)
	case parts[0] == "workloads" && len(parts) == 2 && f.workloads[parts[1]] != nil:
		f.write(w, 200, f.workloads[parts[1]])
	case parts[0] == "runs" && len(parts) == 1 && r.Method == http.MethodPost:
		var object struct {
			Metadata ObjectMeta      `json:"metadata"`
			Spec     json.RawMessage `json:"spec"`
		}
		_ = json.Unmarshal(body, &object)
		f.creates++
		if f.refuseCreate != 0 {
			f.status(w, f.refuseCreate, http.StatusText(f.refuseCreate), "refused")
			return
		}
		if f.rejectCreate != "" {
			f.status(w, 422, "Invalid", f.rejectCreate)
			return
		}
		if existing := f.runs[object.Metadata.Name]; existing != nil {
			if !bytes.Equal(existing.Spec, object.Spec) {
				f.status(w, 409, "AlreadyExists", "different spec")
				return
			}
			f.write(w, 200, existing.Run)
			return
		}
		run := &fakeRun{Spec: object.Spec}
		run.Run.Metadata = object.Metadata
		run.Run.Status.Phase = "Pending"
		f.runs[object.Metadata.Name] = run
		if f.loseCreates > 0 {
			f.loseCreates--
			f.status(w, 503, "ServiceUnavailable", "lost")
			return
		}
		f.write(w, 201, run.Run)
	case parts[0] == "runs" && len(parts) >= 2 && f.runs[parts[1]] != nil:
		run := f.runs[parts[1]]
		switch {
		case len(parts) == 2:
			f.write(w, 200, run.Run)
		case parts[2] == "cancel":
			f.cancels = append(f.cancels, parts[1]+" "+r.Header.Get("Idempotency-Key"))
			f.write(w, 200, run.Run)
		case parts[2] == "events":
			page := EventPage{Items: []Event{}}
			for _, event := range run.Events {
				if event.Sequence > atoi(r.URL.Query().Get("after")) {
					page.Items = append(page.Items, event)
				}
			}
			f.write(w, 200, page)
		case parts[2] == "artifacts":
			_, _ = w.Write([]byte("report"))
		}
	case parts[0] == "decisions" && len(parts) >= 2 && f.decisions[parts[1]] != nil:
		decision := f.decisions[parts[1]]
		if len(parts) == 3 {
			var answer struct{ Answer string }
			_ = json.Unmarshal(body, &answer)
			f.answers = append(f.answers, parts[1]+"="+answer.Answer+" "+r.Header.Get("Idempotency-Key"))
			decision.Spec.Response = &struct {
				Answer     string `json:"answer"`
				AnsweredBy string `json:"answeredBy"`
			}{Answer: answer.Answer}
		}
		f.write(w, 200, decision)
	case parts[0] == "turns" && len(parts) == 1 && r.Method == http.MethodPost:
		var object struct {
			Metadata ObjectMeta `json:"metadata"`
			Spec     TurnSpec   `json:"spec"`
		}
		_ = json.Unmarshal(body, &object)
		f.creates++
		if f.refuseCreate != 0 {
			f.status(w, f.refuseCreate, http.StatusText(f.refuseCreate), "refused")
			return
		}
		if f.turns[object.Metadata.Name] == nil {
			turn := &fakeTurn{Spec: object.Spec}
			turn.Turn.Metadata = object.Metadata
			turn.Turn.Status.Phase = "Pending"
			f.turns[object.Metadata.Name] = turn
		}
		f.write(w, 201, f.turns[object.Metadata.Name].Turn)
	case parts[0] == "turns" && len(parts) == 2 && f.turns[parts[1]] != nil:
		f.write(w, 200, f.turns[parts[1]].Turn)
	default:
		f.status(w, 404, "NotFound", "not found")
	}
}

func atoi(value string) int64 {
	var n int64
	for _, c := range value {
		n = n*10 + int64(c-'0')
	}
	return n
}

func (f *fakeMarina) onlyRun() (string, *fakeRun) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.runs) != 1 {
		f.t.Fatalf("Marina has %d runs", len(f.runs))
	}
	for name, run := range f.runs {
		return name, run
	}
	return "", nil
}

// decide adds an open Decision owned by a Run or Turn.
func (f *fakeMarina) decide(owner, ownerKind, name, kind string, options ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := &Decision{Metadata: ObjectMeta{Name: name, Namespace: "factory"}}
	d.Spec.Owner.Kind, d.Spec.Owner.Name, d.Spec.Kind = ownerKind, owner, kind
	d.Spec.Question, d.Spec.Options, d.Spec.Recommendation = "Approve plan revision 1?", options, ""
	d.Spec.ExpiresAt = "2999-01-01T00:00:00Z"
	if kind == "plan-approval" {
		d.Spec.Recommendation, d.Spec.Details = "Approve plan", "Add a guard and one unit test."
	} else {
		d.Spec.Question = "Which part should I explain?"
	}
	d.Status.Phase = "Open"
	f.decisions[name] = d
	if run := f.runs[owner]; run != nil {
		run.Run.Status.Decisions = append(run.Run.Status.Decisions, name)
	}
	if turn := f.turns[owner]; turn != nil {
		turn.Turn.Status.Decisions = append(turn.Turn.Status.Decisions, name)
	}
}

func (f *fakeMarina) update(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}
