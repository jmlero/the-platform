package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jmlero/the-platform/marina/internal/artifacts"
	"github.com/jmlero/the-platform/marina/internal/conversation"
	"github.com/jmlero/the-platform/marina/internal/deck"
	"github.com/jmlero/the-platform/marina/internal/execution"
	"github.com/jmlero/the-platform/marina/internal/flow"
	"github.com/jmlero/the-platform/marina/internal/httpserver"
	"github.com/jmlero/the-platform/marina/internal/journal/sqlite"
	"github.com/jmlero/the-platform/marina/internal/reconcile"
	"github.com/jmlero/the-platform/marina/internal/workcontrol"
	"github.com/jmlero/the-platform/marina/internal/worker"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout io.Writer, stderr io.Writer) int {
	if len(arguments) == 0 {
		writeUsage(stderr)
		return 2
	}
	if arguments[0] == "serve" || arguments[0] == "doctor" {
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			if strings.HasPrefix(name, "MARINA_APPDECK_") {
				_, _ = fmt.Fprintf(stderr, "rename environment variable %s to %s\n", name, strings.Replace(name, "MARINA_APPDECK_", "MARINA_DECK_", 1))
				return 2
			}
		}
	}

	switch arguments[0] {
	case "check-config":
		return runConfigCheck(arguments[1:], stdout, stderr)
	case "verify":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := worker.RunRemote(ctx); err != nil {
			_, _ = fmt.Fprintln(stderr, "verification failed:", err)
			return 1
		}
		return 0
	case "diagnostics":
		return runDiagnostics(arguments[1:], stdout, stderr)
	case "artifact-retention":
		return runArtifactRetention(arguments[1:], stdout, stderr)
	case "doctor":
		return runDoctor(arguments[1:], stdout, stderr)
	case "serve":
		return runServe(arguments[1:], stderr)
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		writeUsage(stdout)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n\n", arguments[0])
		writeUsage(stderr)
		return 2
	}
}

// Validate offline without reading credentials, opening stores or contacting a
// provider. Error details stay private because decoder errors can quote input.
func runConfigCheck(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("check-config", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", "", "workflow configuration file")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *path == "" {
		_, _ = fmt.Fprintln(stderr, "check-config requires --config and no positional arguments")
		return 2
	}
	configs, err := flow.LoadProjects(*path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "workflow configuration is invalid; private content omitted")
		return 1
	}
	digest := configs[0].Digest()
	if len(configs) > 1 {
		raw, _ := json.Marshal(configs)
		sum := sha256.Sum256(raw)
		digest = hex.EncodeToString(sum[:])
	}
	if err := json.NewEncoder(stdout).Encode(map[string]string{"configuration_digest": digest}); err != nil {
		return 1
	}
	return 0
}

func runDiagnostics(arguments []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	journalPath := flags.String(
		"journal-path",
		envOrDefault("MARINA_JOURNAL_PATH", filepath.Join("data", "marina.db")),
		"SQLite execution journal path",
	)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		_, _ = fmt.Fprintln(stderr, "diagnostics does not accept positional arguments")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	journal, err := sqlite.Open(ctx, *journalPath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "open execution journal: %v\n", err)
		return 1
	}
	defer journal.Close()
	payload, err := journal.ExportDiagnostics(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "export diagnostics: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(payload); err != nil {
		_, _ = fmt.Fprintf(stderr, "write diagnostics: %v\n", err)
		return 1
	}
	return 0
}

func runDoctor(arguments []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	serverURL := flags.String("deck-url", envOrDefault("MARINA_DECK_URL", "http://localhost:8080"), "Deck server URL")
	tokenFile := flags.String(
		"deck-token-file",
		envOrDefault("MARINA_DECK_TOKEN_FILE", ""),
		"file containing the Deck service Bearer token",
	)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}

	client, err := newDeckClient(*serverURL, *tokenFile, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "configure Deck: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Health(ctx); err != nil {
		_, _ = fmt.Fprintf(stderr, "Deck unavailable: %v\n", err)
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "Deck: available")
	return 0
}

func runServe(arguments []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	renewBeforeDefault, err := envDurationOrDefault("MARINA_LEASE_RENEW_BEFORE", 10*time.Second)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "configure lease renewal window: %v\n", err)
		return 2
	}
	runLeaseDefault, err := envDurationOrDefault("MARINA_RUN_LEASE", 30*time.Second)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "configure run lease: %v\n", err)
		return 2
	}
	reconcileIntervalDefault, err := envDurationOrDefault("MARINA_RECONCILE_INTERVAL", 10*time.Second)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "configure reconciliation interval: %v\n", err)
		return 2
	}
	address := flags.String("addr", envOrDefault("MARINA_ADDR", ":8090"), "HTTP listen address")
	configPath := flags.String("config", envOrDefault("MARINA_CONFIG_FILE", ""), "workload configuration file; absent selects control-only mode")
	intakeEnabled := flags.Bool("intake-enabled", true, "discover new work; disabling preserves reconciliation of journaled work")
	intakeTaskID := flags.String("intake-task-id", "", "limit new discovery to one task; existing work still reconciles")
	tlsCertFile := flags.String("tls-cert-file", envOrDefault("MARINA_TLS_CERT_FILE", ""), "server TLS certificate; also available without a workflow")
	tlsKeyFile := flags.String("tls-key-file", envOrDefault("MARINA_TLS_KEY_FILE", ""), "server TLS private key")
	serverURL := flags.String("deck-url", envOrDefault("MARINA_DECK_URL", "http://localhost:8080"), "Deck server URL")
	tokenFile := flags.String(
		"deck-token-file",
		envOrDefault("MARINA_DECK_TOKEN_FILE", ""),
		"file containing the Deck service Bearer token",
	)
	journalPath := flags.String(
		"journal-path",
		envOrDefault("MARINA_JOURNAL_PATH", filepath.Join("data", "marina.db")),
		"SQLite execution journal path",
	)
	renewBefore := flags.Duration(
		"lease-renew-before",
		renewBeforeDefault,
		"window in which restart recovery prioritizes lease renewal",
	)
	runLease := flags.Duration(
		"run-lease",
		runLeaseDefault,
		"Deck lease duration requested by heartbeats",
	)
	reconcileInterval := flags.Duration(
		"reconcile-interval",
		reconcileIntervalDefault,
		"interval between durable lease reconciliation cycles",
	)
	if err := flags.Parse(arguments); err != nil {
		return 2
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	var workflowConfig flow.Config
	var projectConfigs []flow.Config
	requiredCapabilities := workcontrol.MilestoneOneCapabilities()
	if *configPath != "" {
		projectConfigs, err = flow.LoadProjects(*configPath)
		if err != nil {
			logger.Error("load workflow configuration", "error", err)
			return 1
		}
		workflowConfig = projectConfigs[0]
		requiredCapabilities = workcontrol.WorkerCapabilities()
		for _, configured := range projectConfigs {
			if configured.Delivery == "fast_forward" {
				requiredCapabilities = append(requiredCapabilities, workcontrol.CapabilityRunCompleteLanded)
			}
			if configured.Conversation != nil {
				requiredCapabilities = append(requiredCapabilities, workcontrol.CapabilityConversationList, workcontrol.CapabilityConversationContext, workcontrol.CapabilityChannelRead, workcontrol.CapabilityConversationQuestion, workcontrol.CapabilityConversationOutcome, workcontrol.CapabilityConversationPRD, workcontrol.CapabilityConversationPRDTasks)
				if configured.Conversation.Delegation != nil {
					requiredCapabilities = append(requiredCapabilities, workcontrol.CapabilityConversationDelegate)
				}
			}
		}
	}
	if workflowConfig.TLSCertFile != "" {
		if (*tlsCertFile != "" && *tlsCertFile != workflowConfig.TLSCertFile) ||
			(*tlsKeyFile != "" && *tlsKeyFile != workflowConfig.TLSKeyFile) {
			logger.Error("configure server TLS: flags and workflow specify different certificates")
			return 2
		}
		*tlsCertFile, *tlsKeyFile = workflowConfig.TLSCertFile, workflowConfig.TLSKeyFile
	}
	if *tlsCertFile != "" || *tlsKeyFile != "" {
		if _, err := tls.LoadX509KeyPair(*tlsCertFile, *tlsKeyFile); err != nil {
			logger.Error("configure server TLS", "error", err)
			return 2
		}
	}
	client, err := newDeckClient(*serverURL, *tokenFile, &http.Client{Timeout: 5 * time.Second, Transport: deckTransport}, deck.WithRequiredCapabilities(requiredCapabilities))
	if err != nil {
		logger.Error("configure Deck", "error", err)
		return 1
	}
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	providerInfo, compatibilityErr := client.CheckCompatibility(
		startupCtx,
		requiredCapabilities,
	)
	if compatibilityErr != nil {
		logger.Warn(
			"work-control compatibility check failed; service will report not ready",
			"error", compatibilityErr,
		)
	} else {
		logger.Info(
			"work-control compatibility verified",
			"provider", providerInfo.Provider,
			"protocol_version", providerInfo.ProtocolVersion,
			"capability_count", len(providerInfo.Capabilities),
		)
	}
	if *configPath != "" {
		lock, err := artifacts.Lock(workflowConfig.ArtifactDirectory)
		if err != nil {
			logger.Error("lock artifact store", "error", err)
			return 1
		}
		defer lock.Close()
	}
	journal, err := sqlite.Open(startupCtx, *journalPath)
	if err != nil {
		logger.Error("open execution journal", "path", *journalPath, "error", err)
		return 1
	}
	defer func() {
		if err := journal.Close(); err != nil {
			logger.Error("close execution journal", "error", err)
		}
	}()
	identity, err := journal.EnsureInstallationIdentity(startupCtx)
	if err != nil {
		logger.Error("persist stable Marina identity", "error", err)
		return 1
	}
	bindingInput := execution.ProjectBinding{
		ProjectID: envOrDefault("MARINA_DECK_PROJECT_ID", "prj_default"), Provider: "deck",
		Repository: envOrDefault("MARINA_REPOSITORY", "disabled://milestone-one"), Runtime: "disabled:milestone-one",
		PolicyVersion: envOrDefault("MARINA_POLICY_VERSION", "milestone-one"), ContextConfig: "disabled:milestone-one",
		ArtifactConfig: "disabled:milestone-one", DeliveryConfig: "disabled:milestone-one",
		BudgetMinutes: 30, MaxConcurrency: 1,
	}
	if *configPath != "" {
		bindingInput.ProjectID = workflowConfig.ProjectID
		bindingInput.Repository = workflowConfig.Git.CloneURL
		bindingInput.Runtime = "kubernetes:" + workflowConfig.Kubernetes.Namespace
		bindingInput.PolicyVersion = workflowConfig.PolicyVersion
		bindingInput.ContextConfig = workflowConfig.Digest()
		bindingInput.ArtifactConfig = workflowConfig.ArtifactDirectory
		bindingInput.DeliveryConfig = workflowConfig.Git.APIURL
		bindingInput.BudgetMinutes = workflowConfig.BudgetMinutes
	}
	var binding execution.ProjectBinding
	if *configPath != "" {
		binding, err = flow.InitializeRepository(startupCtx, journal, workflowConfig, bindingInput)
	} else {
		binding, err = journal.EnsureProjectBinding(startupCtx, bindingInput)
	}
	if err != nil {
		logger.Error("persist project binding", "error", err)
		return 1
	}
	// Tasks authorized before Deck first confirmed a repository retain the
	// installation's original binding across later association changes.
	if *configPath != "" && workflowConfig.RepositoryEnabled() {
		baseline, baselineErr := journal.FirstRepositoryVersion(startupCtx, binding.ProjectID)
		if baselineErr != nil {
			logger.Error("load original repository binding", "error", baselineErr)
			return 1
		}
		binding.Version = baseline
	}
	projectBindings := []execution.ProjectBinding{binding}
	for index, configured := range projectConfigs {
		if index == 0 {
			continue
		}
		additional, err := initializeProjectBinding(startupCtx, journal, configured)
		if err != nil {
			logger.Error("persist project binding", "error", err)
			return 1
		}
		projectBindings = append(projectBindings, additional)
	}
	controllerEnabled := compatibilityErr == nil
	registration := workcontrol.AgentRegistration{ID: identity.LogicalAgentID,
		InstallationID: identity.InstallationID, Name: "Marina controller", Kind: "controller",
		Capabilities: []string{"work-control", "reconciliation"}}
	for _, configured := range projectConfigs {
		if configured.Conversation != nil {
			registration.Capabilities = append(registration.Capabilities, "conversation.invoke."+configured.ProjectID)
		}
	}
	if controllerEnabled {
		if _, err := client.RegisterAgent(startupCtx, registration); err != nil {
			logger.Error("register stable logical agent", "error", err)
			return 1
		}
	}

	planner, err := reconcile.NewPlanner(journal, *renewBefore)
	if err != nil {
		logger.Error("configure restart reconciliation", "error", err)
		return 1
	}
	leaseReconciler, err := reconcile.NewLeaseReconciler(client, journal, *runLease)
	if err != nil {
		logger.Error("configure provider lease reconciliation", "error", err)
		return 1
	}
	leaseManager, err := reconcile.NewLeaseManager(
		planner,
		leaseReconciler,
		*reconcileInterval,
		*renewBefore,
		1000,
	)
	if err != nil {
		logger.Error("configure continuous lease reconciliation", "error", err)
		return 1
	}
	leaseManager.WithBudgetStopper(leaseReconciler)
	leaseManager.WithRetryJournal(journal)
	controlController, err := reconcile.NewControlController(client, journal, binding, identity, *runLease)
	if err != nil {
		logger.Error("configure milestone-one control controller", "error", err)
		return 1
	}
	for _, configured := range projectBindings {
		controlController.WithProjectBinding(configured)
	}
	for _, configured := range projectConfigs {
		controlController.WithProjectExecutionLimits(configured.ProjectID, flow.RepositoryRuntimeLimit, configured.MaxTokens)
	}
	controlController.WithIntakeEnabled(*intakeEnabled)
	controlController.WithIntakeWorkID(*intakeTaskID)
	if !*intakeEnabled {
		logger.Info("new work intake disabled; journaled work continues to reconcile")
	}
	var workflow *flow.Controller
	var workerHandler http.Handler
	var conversationController *conversation.Controller
	var conversationHandler http.Handler
	if *configPath != "" {
		workflow, workerHandler, err = configureWorkers(workflowConfig, journal, client)
		if err != nil {
			logger.Error("configure repository workflow", "error", err)
			return 1
		}
		controlController.WithExecutionLimits(flow.RepositoryRuntimeLimit, workflowConfig.MaxTokens).WithCommandHandler(workflow.Command)
		var closeConversation func()
		conversationController, conversationHandler, closeConversation, err = configureConversation(workflowConfig, journal, client, identity.LogicalAgentID)
		if err != nil {
			logger.Error("configure conversation workflow", "error", err)
			return 1
		}
		defer closeConversation()
	}
	workflows := []*flow.Controller{}
	conversations := []*conversation.Controller{}
	workerHandlers := map[string]http.Handler{}
	conversationHandlers := map[string]http.Handler{}
	if workflow != nil {
		workflows = append(workflows, workflow)
		workerHandlers[workflowConfig.ProjectID] = workerHandler
	}
	if conversationController != nil {
		conversations = append(conversations, conversationController)
		conversationHandlers[workflowConfig.ProjectID] = conversationHandler
	}
	for index, configured := range projectConfigs {
		if index == 0 {
			continue
		}
		projectWorkflow, gateway, err := configureWorkers(configured, journal, client)
		if err != nil {
			logger.Error("configure project workflow", "error", err)
			return 1
		}
		workflows = append(workflows, projectWorkflow)
		workerHandlers[configured.ProjectID] = gateway
		projectConversation, chatGateway, closeChat, err := configureConversation(configured, journal, client, identity.LogicalAgentID)
		if err != nil {
			logger.Error("configure project conversation", "error", err)
			return 1
		}
		defer closeChat()
		if projectConversation != nil {
			conversations = append(conversations, projectConversation)
			conversationHandlers[configured.ProjectID] = chatGateway
		}
	}
	if len(workflows) > 0 {
		controlController.WithCommandHandler(func(ctx context.Context, e execution.Execution, cmd workcontrol.Command) (string, string, error) {
			for _, configured := range workflows {
				if configured.Config.ProjectID == e.ProjectID {
					return configured.Command(ctx, e, cmd)
				}
			}
			return "", "", errProjectNotConfigured
		})
	}
	if len(projectConfigs) > 1 {
		gateway := projectGateway(journal, workerHandlers, conversationHandlers, workerHandler)
		workerHandler = gateway
		conversationHandler = gateway
	}
	if controllerEnabled {
		for _, workflow := range workflows {
			if err := workflow.PublishWorkloads(startupCtx); err != nil {
				logger.Warn("workload discovery publication deferred", "error", err)
			}
			if err := workflow.ReconcileRepository(startupCtx); err != nil {
				logger.Warn("repository association deferred", "error", err)
			}
		}
		startupControl := controlController.Cycle(startupCtx)
		logControlCycle(logger, startupControl, "startup")
		startupCycle := leaseManager.Cycle(startupCtx)
		logLeaseCycle(logger, startupCycle, "startup")
		if startupCycle.Err != nil {
			return 1
		}
	}

	healthHandler := httpserver.New(httpserver.Dependencies{WorkControl: client, Journal: journal})
	mux := http.NewServeMux()
	mux.Handle("/", healthHandler)
	if workerHandler != nil {
		mux.Handle("/workers/", workerHandler)
		mux.Handle("/engine/", workerHandler)
		mux.Handle("/artifacts/", workerHandler)
	}
	if conversationHandler != nil {
		mux.Handle("/conversation/", conversationHandler)
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	var controllers sync.WaitGroup
	controllers.Add(1)
	go func() {
		defer controllers.Done()
		ticker := time.NewTicker(*reconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, workflow := range workflows {
					if err := workflow.StopExpired(ctx); err != nil {
						logger.Warn("expired runtime termination deferred", "error", err)
					}
				}
				if !controllerEnabled {
					operationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, compatibilityErr := client.CheckCompatibility(operationCtx, requiredCapabilities)
					if compatibilityErr == nil {
						_, compatibilityErr = client.RegisterAgent(operationCtx, registration)
					}
					cancel()
					if compatibilityErr != nil {
						logger.Warn("milestone-one controllers remain disabled", "error", compatibilityErr)
						continue
					}
					controllerEnabled = true
					logger.Info("milestone-one controllers enabled after provider recovery")
				}
				for _, workflow := range workflows {
					op, cancel := context.WithTimeout(ctx, 30*time.Second)
					if err := workflow.PublishWorkloads(op); err != nil {
						logger.Warn("workload discovery publication deferred", "error", err)
					}
					err := workflow.ReconcileRepository(op)
					cancel()
					if err != nil {
						logger.Warn("repository association deferred", "error", err)
					}
				}
				logControlCycle(logger, controlController.Cycle(ctx), "scheduled")
				logLeaseCycle(logger, leaseManager.Cycle(ctx), "scheduled")
				if workflow != nil {
					for _, err := range workflow.Cycle(ctx) {
						logger.Warn("repository workflow deferred", "error", err)
					}
				}
				for _, conversationController := range conversations {
					if ctx.Err() != nil {
						return
					}
					cycleCtx, cancel := conversationCycleContext(ctx)
					for _, err := range conversationController.Cycle(cycleCtx) {
						logger.Warn("conversation workflow deferred", "error", err)
					}
					cancel()
				}
			}
		}
	}()
	defer func() {
		stop()
		controllers.Wait()
	}()

	result := make(chan error, 1)
	go func() {
		logger.Info(
			"Marina HTTP service starting",
			"address", *address,
			"deck_url", *serverURL,
			"journal_path", *journalPath,
			"reconcile_interval", reconcileInterval.String(),
		)
		if *tlsCertFile != "" {
			result <- server.ListenAndServeTLS(*tlsCertFile, *tlsKeyFile)
		} else {
			result <- server.ListenAndServe()
		}
	}()

	select {
	case err := <-result:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Marina HTTP service stopped", "error", err)
			return 1
		}
		return 0
	case <-ctx.Done():
		logger.Info("Marina HTTP service shutting down")
	}

	if err := drainControllersAndShutdown(server, &controllers); err != nil {
		logger.Error("Marina HTTP service shutdown failed", "error", err)
		return 1
	}
	if err := <-result; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("Marina HTTP service stopped", "error", err)
		return 1
	}
	return 0
}

// Keep established gateway streams alive while a replacement Pod starts.
// Conversation calls are bounded to five minutes by the installed profile;
// Kubernetes gives this process an additional 30 seconds to finish draining.
func shutdownHTTPServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return server.Shutdown(ctx)
}

// A termination signal stops new reconciliation cycles, while the current
// conversation cycle keeps its own deadline and can complete through the
// gateway before the old Pod closes its HTTP listener.
func conversationCycleContext(signalContext context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(signalContext), 30*time.Minute)
}

func drainControllersAndShutdown(server *http.Server, controllers *sync.WaitGroup) error {
	controllers.Wait()
	return shutdownHTTPServer(server)
}

func logControlCycle(logger *slog.Logger, cycle reconcile.ControlCycle, trigger string) {
	logger.Info("milestone-one control cycle completed", "trigger", trigger,
		"acquisitions", cycle.Acquisitions, "commands", cycle.Commands, "published", cycle.Published,
		"error_count", len(cycle.Errors))
	for _, err := range cycle.Errors {
		logger.Warn("milestone-one control operation deferred", "trigger", trigger, "error", err)
	}
}

func logLeaseCycle(logger *slog.Logger, cycle reconcile.LeaseCycle, trigger string) {
	if cycle.Err != nil {
		logger.Error(
			"lease reconciliation cycle could not load durable executions",
			"trigger", trigger,
			"error", cycle.Err,
		)
		return
	}
	logger.Info(
		"lease reconciliation cycle planned",
		"trigger", trigger,
		"intent_count", len(cycle.Attempts),
	)
	for _, attempt := range cycle.Attempts {
		logger.Info(
			"durable recovery intent evaluated",
			"execution_id", attempt.Intent.ExecutionID,
			"action", attempt.Intent.Action,
			"reason", attempt.Intent.Reason,
			"provider_reconciliation_attempted", attempt.Attempted,
		)
		if !attempt.Attempted {
			continue
		}
		if attempt.Err != nil {
			logger.Error(
				"external run ownership could not be reconciled",
				"execution_id", attempt.Intent.ExecutionID,
				"error", attempt.Err,
			)
			continue
		}
		if attempt.Intent.Action == execution.RecoveryStopBudget {
			logger.Warn(
				"budget-exhausted execution stopped",
				"execution_id", attempt.Intent.ExecutionID,
				"state", attempt.Execution.State,
				"failure_class", attempt.Execution.FailureClass,
				"journal_version", attempt.Execution.Version,
			)
			continue
		}
		logger.Info(
			"external run ownership reconciled",
			"execution_id", attempt.Intent.ExecutionID,
			"status", attempt.Result.Status,
			"provider_run_status", attempt.Result.ProviderRunStatus,
			"lease_expires_at", attempt.Result.LeaseExpiresAt,
			"journal_version", attempt.Result.LocalVersion,
		)
	}
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func newDeckClient(serverURL string, tokenFile string, httpClient *http.Client, additional ...deck.Option) (*deck.Client, error) {
	options := append([]deck.Option(nil), additional...)
	if strings.TrimSpace(tokenFile) != "" {
		options = append(options, deck.WithBearerTokenFile(tokenFile))
	}
	return deck.New(serverURL, httpClient, options...)
}

func envDurationOrDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return parsed, nil
}

func writeUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, "Usage: marina <command> [options]")
	_, _ = fmt.Fprintln(writer)
	_, _ = fmt.Fprintln(writer, "Commands:")
	_, _ = fmt.Fprintln(writer, "  check-config  Validate workflow configuration offline without exposing private content")
	_, _ = fmt.Fprintln(writer, "  verify   Run one independent clean verification attempt")
	_, _ = fmt.Fprintln(writer, "  diagnostics  Export a deterministic, redacted view of the local journal")
	_, _ = fmt.Fprintln(writer, "  artifact-retention  Plan or apply offline cleanup of expired unreferenced artifacts")
	_, _ = fmt.Fprintln(writer, "  doctor   Check the configured work-control dependency")
	_, _ = fmt.Fprintln(writer, "  serve    Run Marina's HTTP liveness and readiness service")
	_, _ = fmt.Fprintln(writer, "  version  Print the Marina version")
}
