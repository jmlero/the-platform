// Package workerapi is the bounded data boundary for disposable workers. Its
// credentials grant access to one attempt, never to Deck or Git publication.
package workerapi

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/jmlero/the-platform/marina/internal/analysis"
	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
	"github.com/jmlero/the-platform/marina/internal/workresult"
)

type Model struct {
	Endpoint               string `json:"endpoint"`
	Name                   string `json:"name"`
	TokenFile              string `json:"token_file"`
	MaxOutputTokens        int64  `json:"max_output_tokens"`
	InputMicrosPerMillion  int64  `json:"input_micros_per_million"`
	OutputMicrosPerMillion int64  `json:"output_micros_per_million"`
	Currency               string `json:"currency"`
}

type Config struct {
	ContextCheck      func(context.Context, execution.Execution) error
	SourceBinding     func(context.Context, execution.Execution) (string, string, error)
	PublicURL         string
	SigningKey        []byte
	SourceURL         string
	SourceTokenFile   string
	ArtifactTokenFile string
	Model             Model
}

type Server struct {
	config    Config
	journal   *sqlite.Store
	artifacts *artifacts.Store
	provider  workcontrol.WorkInspection
	client    *http.Client
	mux       *http.ServeMux
}

var errProviderUnavailable = errors.New("work-control provider unavailable")

func New(config Config, journal *sqlite.Store, store *artifacts.Store, provider workcontrol.WorkInspection, client *http.Client) *Server {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	server := &Server{config: config, journal: journal, artifacts: store, provider: provider, client: &copyClient, mux: http.NewServeMux()}
	server.mux.HandleFunc("GET /workers/{id}/input", server.input)
	server.mux.HandleFunc("GET /workers/{id}/authority", server.authority)
	server.mux.HandleFunc("POST /workers/{id}/result", server.result)
	server.mux.HandleFunc("GET /workers/{id}/git/info/refs", server.source)
	server.mux.HandleFunc("POST /workers/{id}/git/git-upload-pack", server.source)
	server.mux.HandleFunc("POST /workers/{id}/model/v1/responses", server.model)
	server.mux.HandleFunc("GET /artifacts/{digest}", server.artifact)
	server.mux.HandleFunc("POST /workers/{id}/tools/read-context-source", server.contextTool)
	server.mux.HandleFunc("POST /engine/model/v1/responses", server.engineRoute(server.model))
	server.mux.HandleFunc("GET /engine/input", server.engineRoute(server.input))
	server.mux.HandleFunc("GET /engine/authority", server.engineRoute(server.authority))
	server.mux.HandleFunc("GET /engine/git/info/refs", server.engineRoute(server.source))
	server.mux.HandleFunc("POST /engine/git/git-upload-pack", server.engineRoute(server.source))
	server.mux.HandleFunc("POST /engine/tools/read-context-source", server.engineRoute(server.contextTool))
	return server
}

func (server *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { server.mux.ServeHTTP(w, r) }

func (server *Server) authorize(ctx context.Context, id, token string) (runtimeapi.Attempt, execution.Execution, error) {
	attempt, err := server.journal.GetRuntimeAttempt(ctx, id)
	if err != nil {
		return attempt, execution.Execution{}, err
	}
	want := kubernetes.Token(server.config.SigningKey, attempt.Specification)
	if len(server.config.SigningKey) < 32 || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return attempt, execution.Execution{}, errors.New("invalid worker credential")
	}
	if attempt.Specification.Engine != nil {
		engine, err := server.journal.GetEngineAttempt(ctx, id)
		if err != nil || !engine.AllowsCalls() {
			return attempt, execution.Execution{}, errors.New("engine authority unavailable or revoked")
		}
	}
	return server.authorizeState(ctx, attempt)
}

func (server *Server) authorizeState(ctx context.Context, attempt runtimeapi.Attempt) (runtimeapi.Attempt, execution.Execution, error) {
	record, err := server.journal.GetExecution(ctx, attempt.ExecutionID)
	if err != nil {
		return attempt, record, err
	}
	now := time.Now().UTC()
	if attempt.Specification.Engine != nil {
		ref, err := attempt.Specification.EngineGatewayReference()
		if err != nil || !now.Before(ref.ExpiresAt) {
			return attempt, record, errors.New("engine credential expired or not attempt-scoped")
		}
	}
	if record.State.Terminal() || attempt.State.Terminal() || record.FenceToken != attempt.Specification.AuthorityFence ||
		!now.Before(record.DeadlineAt) || !now.Before(attempt.DeadlineAt) || !now.Before(record.LeaseExpiresAt) {
		return attempt, record, errors.New("worker authority expired")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	detail, err := server.provider.GetWork(checkCtx, record.ExternalWorkID)
	if err != nil {
		return attempt, record, fmt.Errorf("%w: %v", errProviderUnavailable, err)
	}
	for _, run := range detail.Runs {
		if run.ID == record.ExternalRunID && run.WorkItemID == record.ExternalWorkID && run.AgentID == record.AgentID && run.TaskVersion == record.TaskVersion &&
			run.FenceToken == attempt.Specification.AuthorityFence && run.Status.Active() && run.LeaseExpiresAt != nil && now.Before(*run.LeaseExpiresAt) {
			record.LeaseExpiresAt = *run.LeaseExpiresAt
			if attempt.Specification.Engine != nil && server.config.ContextCheck == nil {
				return attempt, record, errors.New("engine requires current context validation")
			}
			if server.config.ContextCheck != nil {
				if err := server.config.ContextCheck(checkCtx, record); err != nil {
					return attempt, record, err
				}
			}
			return attempt, record, nil
		}
	}
	return attempt, record, errors.New("provider revoked worker authority")
}

func (server *Server) admit(w http.ResponseWriter, r *http.Request) (runtimeapi.Attempt, execution.Execution, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		http.Error(w, "worker credential required", 401)
		return runtimeapi.Attempt{}, execution.Execution{}, false
	}
	attempt, record, err := server.authorize(r.Context(), r.PathValue("id"), token)
	if err != nil {
		if errors.Is(err, errProviderUnavailable) || errors.Is(err, workcontrol.ErrContextUnavailable) {
			http.Error(w, "worker authority cannot currently be confirmed", http.StatusServiceUnavailable)
			return attempt, record, false
		}
		http.Error(w, "worker authority is unavailable or revoked", 403)
		return attempt, record, false
	}
	return attempt, record, true
}

func (server *Server) Input(ctx context.Context, attempt runtimeapi.Attempt) (worker.Input, error) {
	checkpoint, err := server.journal.GetCheckpoint(ctx, attempt.ExecutionID, "worker-input:"+attempt.ID)
	if err != nil {
		return worker.Input{}, err
	}
	encoded, ok := checkpoint.Payload["input"].(string)
	if !ok {
		return worker.Input{}, errors.New("worker input is missing")
	}
	var input worker.Input
	err = json.Unmarshal([]byte(encoded), &input)
	if err == nil && input.Context != nil && (input.Context.Digest != attempt.Specification.ContextDigest || input.Context.ExecutionID != attempt.ExecutionID || input.Context.Validate() != nil) {
		return worker.Input{}, errors.New("worker context differs from immutable specification")
	}
	if err == nil && input.Stage == "analyze" {
		if input.Analysis == nil || input.Analysis.Version != 1 || attempt.Specification.Workload != analysis.Kind ||
			input.Analysis.InputDigest != attempt.Specification.InputDigest || input.Analysis.ProfileDigest != attempt.Specification.ProfileDigest ||
			input.Analysis.InputDigest != analysis.Digest(input.Analysis.Sources) || !slices.Equal(input.Analysis.Tools, attempt.Specification.Tools) || !reflect.DeepEqual(input.Analysis.Context, input.Context) {
			return worker.Input{}, errors.New("analysis input differs from its immutable specification")
		}
	}
	return input, err
}

func (server *Server) input(w http.ResponseWriter, r *http.Request) {
	attempt, _, ok := server.admit(w, r)
	if !ok {
		return
	}
	input, err := server.Input(r.Context(), attempt)
	if err != nil {
		http.Error(w, "worker input is unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(input)
}

func (server *Server) authority(w http.ResponseWriter, r *http.Request) {
	attempt, record, ok := server.admit(w, r)
	if !ok {
		return
	}
	expires := record.LeaseExpiresAt
	if record.DeadlineAt.Before(expires) {
		expires = record.DeadlineAt
	}
	if attempt.DeadlineAt.Before(expires) {
		expires = attempt.DeadlineAt
	}
	if attempt.Specification.Engine != nil {
		ref, err := attempt.Specification.EngineGatewayReference()
		if err == nil && ref.ExpiresAt.Before(expires) {
			expires = ref.ExpiresAt
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"expires_at": expires, "fence": record.FenceToken})
}

func (server *Server) result(w http.ResponseWriter, r *http.Request) {
	attempt, _, ok := server.admit(w, r)
	if !ok {
		return
	}
	if attempt.Specification.Engine != nil {
		http.Error(w, "engine results require artifact collection", 403)
		return
	}
	input, err := server.Input(r.Context(), attempt)
	if err != nil {
		http.Error(w, "worker input is unavailable", 503)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, worker.MaxResultBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid worker result", 400)
		return
	}
	want := workresult.Expected{Stage: input.Stage, SourceRevision: input.SourceRevision, AllowedPaths: input.AllowedPaths, Checks: input.Checks,
		NotBefore: attempt.CreatedAt.Add(-time.Minute), Now: time.Now()}
	if input.Stage == "analyze" {
		if input.Analysis == nil {
			http.Error(w, "analysis input is unavailable", 503)
			return
		}
		want.InputDigest, want.ProfileDigest = input.Analysis.InputDigest, input.Analysis.ProfileDigest
	}
	result, err := workresult.Parse(data, want)
	if err != nil {
		http.Error(w, "result does not match the immutable worker input", 409)
		return
	}
	encoded, _ := json.Marshal(result)
	digest, err := server.artifacts.Put(encoded)
	if err != nil {
		http.Error(w, "artifact storage is unavailable", 503)
		return
	}
	_, _, err = server.journal.RecordRuntimeArtifact(r.Context(), runtimeapi.Artifact{ID: "result:" + attempt.ID, ExecutionID: attempt.ExecutionID, RuntimeAttemptID: attempt.ID,
		Name: input.Stage + ".json", Kind: "worker_result", URI: strings.TrimRight(server.config.PublicURL, "/") + "/artifacts/" + digest, Digest: digest, SizeBytes: int64(len(encoded)), CreatedAt: result.ObservedAt})
	if errors.Is(err, runtimeapi.ErrConflict) {
		http.Error(w, "worker result is immutable", 409)
		return
	}
	if err != nil {
		http.Error(w, "artifact journal is unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"digest": digest})
}

func (server *Server) artifact(w http.ResponseWriter, r *http.Request) {
	if server.config.ArtifactTokenFile == "" {
		http.Error(w, "artifact access is not configured", 503)
		return
	}
	token, err := os.ReadFile(server.config.ArtifactTokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		http.Error(w, "artifact credential unavailable", 503)
		return
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(strings.TrimSpace(string(token)))) != 1 {
		http.Error(w, "artifact credential required", 401)
		return
	}
	value, err := server.artifacts.Get(r.PathValue("digest"))
	if err != nil {
		http.Error(w, "artifact unavailable", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="marina-result.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(value)
}

func (server *Server) liveContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _, err := server.authorize(ctx, r.PathValue("id"), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func (server *Server) source(w http.ResponseWriter, r *http.Request) {
	attempt, executionRecord, ok := server.admit(w, r)
	if !ok {
		return
	}
	if attempt.Specification.Workload == analysis.Kind || attempt.Specification.RepositoryURI == "" {
		http.Error(w, "this workload has no source authority", 403)
		return
	}
	sourceURL, tokenFile := server.config.SourceURL, server.config.SourceTokenFile
	if server.config.SourceBinding != nil {
		var err error
		sourceURL, tokenFile, err = server.config.SourceBinding(r.Context(), executionRecord)
		if err != nil {
			http.Error(w, "original repository binding unavailable", 503)
			return
		}
	}
	if attempt.Specification.RepositoryURI != sourceURL {
		http.Error(w, "repository binding changed", 409)
		return
	}
	path := "/git-upload-pack"
	if r.Method == http.MethodGet {
		if r.URL.RawQuery != "service=git-upload-pack" {
			http.Error(w, "only source reads are allowed", 403)
			return
		}
		path = "/info/refs?service=git-upload-pack"
	} else if r.URL.RawQuery != "" {
		http.Error(w, "unexpected source query", 400)
		return
	}
	// Git gzips large upload-pack requests, such as a full clone; the encoding
	// must reach the provider with the bytes it describes.
	encoding := r.Header.Get("Content-Encoding")
	if encoding != "" && encoding != "gzip" {
		http.Error(w, "unsupported source request encoding", 400)
		return
	}
	ctx, cancel := server.liveContext(r)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, r.Method, strings.TrimRight(sourceURL, "/")+path, http.MaxBytesReader(w, r.Body, 16<<20))
	if err != nil {
		http.Error(w, "source endpoint unavailable", 503)
		return
	}
	request.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	request.Header.Set("Git-Protocol", r.Header.Get("Git-Protocol"))
	if accept := r.Header.Get("Accept"); accept != "" {
		request.Header.Set("Accept", accept)
	}
	if encoding != "" {
		request.Header.Set("Content-Encoding", encoding)
	}
	if tokenFile != "" {
		token, err := os.ReadFile(tokenFile)
		if err != nil || strings.TrimSpace(string(token)) == "" {
			http.Error(w, "source credential unavailable", 503)
			return
		}
		request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("x-access-token:"+strings.TrimSpace(string(token)))))
	}
	response, err := server.client.Do(request)
	if err != nil {
		http.Error(w, "source provider unavailable", 502)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		http.Error(w, "source provider rejected the read", 502)
		return
	}
	w.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, io.LimitReader(response.Body, 128<<20))
}
