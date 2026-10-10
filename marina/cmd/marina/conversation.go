package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversation"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/kagent"
	"github.com/jmlero/the-platform/marina/internal/kubernetes"
	runtime "github.com/jmlero/the-platform/marina/internal/runtime"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol"
	"github.com/jmlero/the-platform/marina/internal/sourcecontrol/github"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func configureConversation(config flow.Config, journal *sqlite.Store, source workcontrol.ConversationSource, agentID string) (*conversation.Controller, http.Handler, func(), error) {
	if config.Conversation == nil {
		return nil, nil, func() {}, nil
	}
	key, err := os.ReadFile(config.SigningKeyFile)
	key = []byte(strings.TrimSpace(string(key)))
	if err != nil || len(key) < 32 {
		return nil, nil, nil, errors.New("conversation signing key unavailable")
	}
	blobs, err := artifacts.New(config.ArtifactDirectory)
	if err != nil {
		return nil, nil, nil, err
	}
	kubeConfig := config.Kubernetes
	kubeConfig.WorkerURL = config.PublicURL
	kubeConfig.SigningKey = key
	kube, err := kubernetes.New(kubeConfig, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	endpoint := config.Conversation.Engine.Endpoint
	conn, err := grpc.NewClient(endpoint, append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, engineDialOptions...)...)
	if err != nil {
		return nil, nil, nil, err
	}
	gateway := conversation.NewGateway(journal, key, config.Model, config.Conversation.Digest(config.Model), config.PolicyVersion, modelClient())
	gateway.Blobs, gateway.Source = blobs, source
	gateway.ProjectID = config.ProjectID
	if config.RepositoryEnabled() {
		gateway.Repository = func(ctx context.Context, binding workcontrol.RepositoryBinding) (sourcecontrol.RepositoryReader, error) {
			selected, profile, err := journal.RepositorySnapshot(ctx, config.ProjectID, binding.Version)
			if err != nil {
				return nil, err
			}
			if selected != binding || profile != config.Digest() {
				return nil, errors.New("conversation repository binding changed or is unavailable")
			}
			git := config.Git
			git.Repository, git.CloneURL, git.BaseBranch = binding.Repository, binding.CloneURL, binding.BaseBranch
			// This client has no mutation authorization and never leaves Marina.
			git.Authorize, git.ReadBlob = nil, nil
			return github.New(git, sourceClient())
		}
	}
	controller := &conversation.Controller{ProjectID: config.ProjectID, AgentID: agentID, PolicyVersion: config.PolicyVersion, DelegationProfileDigest: config.Digest(), DelegationProfileID: config.RepositoryProfileID(),
		Profile: *config.Conversation, Model: config.Model, Source: source, Repository: gateway.Repository, Journal: journal, Blobs: blobs,
		Engine: kagent.New(conn, endpoint, journal.ConversationEngineJournal(), blobs, gateway), Templates: kube, Gateway: gateway,
		InstallCredential: func(ctx context.Context, spec runtime.Specification, credential string) error {
			return kube.InstallEngineCredential(ctx, spec, credential)
		}}
	return controller, gateway, func() { _ = conn.Close() }, nil
}
