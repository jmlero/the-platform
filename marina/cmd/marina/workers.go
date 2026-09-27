package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtimeapi "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workerapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func configureWorkers(config flow.Config, journal *sqlite.Store, provider flow.Provider) (*flow.Controller, http.Handler, error) {
	if config.TLSCertFile != "" {
		if _, err := tls.LoadX509KeyPair(config.TLSCertFile, config.TLSKeyFile); err != nil {
			return nil, nil, fmt.Errorf("load worker gateway TLS: %w", err)
		}
	}
	for _, path := range []string{config.Git.TokenFile, config.Model.TokenFile} {
		if path == "" {
			continue
		}
		value, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(value)) == "" {
			return nil, nil, fmt.Errorf("configured provider credential file is unavailable or empty")
		}
	}
	key, err := os.ReadFile(config.SigningKeyFile)
	if err != nil {
		return nil, nil, err
	}
	key = []byte(strings.TrimSpace(string(key)))
	if len(key) < 32 {
		return nil, nil, fmt.Errorf("worker signing key requires at least 32 bytes")
	}
	token, err := os.ReadFile(config.ArtifactTokenFile)
	if err != nil || len(strings.TrimSpace(string(token))) < 32 {
		return nil, nil, fmt.Errorf("artifact credential requires at least 32 bytes")
	}
	store, err := artifacts.New(config.ArtifactDirectory)
	if err != nil {
		return nil, nil, err
	}
	kube := config.Kubernetes
	kube.WorkerURL = config.PublicURL
	kube.SigningKey = key
	if config.Analysis != nil {
		kube.AdditionalImages = []string{config.Analysis.Image}
		if !config.RepositoryEnabled() {
			kube.Image = config.Analysis.Image
		}
	}
	runtime, err := kubernetes.New(kube, nil)
	if err != nil {
		return nil, nil, err
	}
	controller := &flow.Controller{Config: config, Journal: journal, Provider: provider, Runtime: runtime, Artifacts: store}
	controller.BindingConfig = controller.ConfigurationForExecution
	controller.Source = func(e execution.Execution) (sourcecontrol.Port, error) {
		effective, err := controller.ConfigurationForExecution(context.Background(), e)
		if err != nil {
			return nil, err
		}
		git := effective.Git
		git.ReadBlob = store.Get
		git.CommitTime = e.CreatedAt
		git.Authorize = func(ctx context.Context, fence int64) error { return controller.Authorize(ctx, e, fence) }
		return github.New(git, sourceClient())
	}
	if config.RepositoryEnabled() {
		if _, err := github.New(config.Git, nil); err != nil {
			return nil, nil, err
		}
	}
	gateway := workerapi.New(workerapi.Config{SourceBinding: func(ctx context.Context, e execution.Execution) (string, string, error) {
		effective, err := controller.ConfigurationForExecution(ctx, e)
		return effective.Git.CloneURL, effective.Git.TokenFile, err
	}, ContextCheck: controller.CheckContext, PublicURL: config.PublicURL, SigningKey: key, SourceURL: config.Git.CloneURL, SourceTokenFile: config.Git.TokenFile, ArtifactTokenFile: config.ArtifactTokenFile, Model: config.Model}, journal, store, provider, nil)
	// Retain cancellation and inspection for a journaled pilot attempt even
	// after the profile stops selecting the engine for new work.
	const engineEndpoint = "kint-engine-controller.kint-engine.svc:8083"
	conn, err := grpc.NewClient(engineEndpoint, append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, engineDialOptions...)...)
	if err != nil {
		return nil, nil, err
	}
	controller.EngineRuntime = kagent.New(conn, engineEndpoint, journal, store, gateway)
	controller.EngineTemplates = runtime
	controller.PrepareEngine = func(ctx context.Context, spec runtimeapi.Specification) error {
		credential, err := gateway.EngineCredential(ctx, spec)
		if err != nil {
			return err
		}
		return runtime.InstallEngineCredential(ctx, spec, credential)
	}
	return controller, gateway, nil
}
