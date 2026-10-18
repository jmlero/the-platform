package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/jmlero/the-platform/deck/internal/httpapi"
	"github.com/jmlero/the-platform/deck/internal/marina"
	"github.com/jmlero/the-platform/deck/internal/store"
	"github.com/jmlero/the-platform/deck/internal/webui"
)

// defaultMarinaTokenFile is the projected service account token in a Pod.
const defaultMarinaTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(env("DECK_LOG_LEVEL", "info")),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dataDir := env("DECK_DATA_DIR", "./data")
	if err := validateRenamedConfiguration(os.Environ(), dataDir); err != nil {
		logger.Error("upgrade Deck configuration", "error", err)
		os.Exit(1)
	}
	databasePath := filepath.Join(dataDir, "deck.db")
	appStore, err := store.Open(ctx, databasePath)
	if err != nil {
		logger.Error("open Deck store", "error", err)
		os.Exit(1)
	}
	defer appStore.Close()

	for _, obsolete := range []string{"DECK_DEMO_AGENT", "DECK_SERVICE_AUTH_FILE", "DECK_ARTIFACT_URL", "DECK_ARTIFACT_TOKEN_FILE", "DECK_ARTIFACT_CA_FILE"} {
		if os.Getenv(obsolete) != "" {
			logger.Warn("ignoring obsolete setting; Deck now calls Marina's platform API", "variable", obsolete)
		}
	}
	httpOptions := make([]httpapi.Option, 0, 3)
	marinaURL := strings.TrimSpace(os.Getenv("DECK_MARINA_URL"))
	if marinaURL != "" {
		client, err := marina.New(marina.Config{URL: marinaURL, TokenFile: env("DECK_MARINA_TOKEN_FILE", defaultMarinaTokenFile), CAFile: strings.TrimSpace(os.Getenv("DECK_MARINA_CA_FILE"))})
		if err != nil {
			logger.Error("configure Marina", "error", err)
			os.Exit(1)
		}
		projectsFile := strings.TrimSpace(os.Getenv("DECK_MARINA_PROJECTS_FILE"))
		if _, err := marina.LoadProjects(projectsFile); err != nil {
			logger.Error("load Marina project mapping", "error", err)
			os.Exit(1)
		}
		httpOptions = append(httpOptions, httpapi.WithMarina(client))
		go (&marina.Engine{Store: appStore, Client: client, ProjectsFile: projectsFile, Log: logger}).Run(ctx)
	}
	operatorAuthFile := strings.TrimSpace(os.Getenv("DECK_OPERATOR_AUTH_FILE"))
	if operatorAuthFile != "" {
		authenticator, err := httpapi.NewFileOperatorAuthenticator(operatorAuthFile)
		if err != nil {
			logger.Error("load operator authentication policy", "error", err)
			os.Exit(1)
		}
		httpOptions = append(
			httpOptions,
			httpapi.WithOperatorAuthenticator(authenticator),
			httpapi.WithSecureOperatorCookies(envBool("DECK_OPERATOR_COOKIE_SECURE", true)),
		)
	}

	api := httpapi.New(appStore, logger, httpOptions...).Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/healthz", api)
	mux.Handle("/readyz", api)
	mux.Handle("/", webui.Handler())

	server := &http.Server{
		Addr:              env("DECK_ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info(
			"Deck is ready",
			"address", server.Addr,
			"database", databasePath,
			"marina", marinaURL != "",
			"operator_auth", operatorAuthFile != "",
		)
		var serveErr error
		if certificate := os.Getenv("DECK_TLS_CERT_FILE"); certificate != "" {
			serveErr = server.ListenAndServeTLS(certificate, os.Getenv("DECK_TLS_KEY_FILE"))
		} else {
			serveErr = server.ListenAndServe()
		}
		if err := serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve Deck", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown Deck", "error", err)
	}
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	if value == "" {
		return fallback
	}
	return value != "false" && value != "0" && value != "no"
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(value) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
