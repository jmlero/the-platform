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
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, err
	}
	gateway := conversation.NewGateway(journal, key, config.Model, config.Conversation.Digest(config.Model), config.PolicyVersion, nil)
	controller := &conversation.Controller{ProjectID: config.ProjectID, AgentID: agentID, PolicyVersion: config.PolicyVersion, DelegationProfileDigest: config.Digest(), DelegationProfileID: config.RepositoryProfileID(),
		Profile: *config.Conversation, Model: config.Model, Source: source, Journal: journal, Blobs: blobs,
		Engine: kagent.New(conn, endpoint, journal.ConversationEngineJournal(), blobs, gateway), Templates: kube, Gateway: gateway,
		InstallCredential: func(ctx context.Context, spec runtime.Specification, credential string) error {
			return kube.InstallEngineCredential(ctx, spec, credential)
		}}
	return controller, gateway, func() { _ = conn.Close() }, nil
}
