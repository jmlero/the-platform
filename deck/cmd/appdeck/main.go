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

	"github.com/jmlero/the-platform/deck/internal/demo"
	"github.com/jmlero/the-platform/deck/internal/httpapi"
	"github.com/jmlero/the-platform/deck/internal/store"
	"github.com/jmlero/the-platform/deck/internal/webui"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(env("APPDECK_LOG_LEVEL", "info")),
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dataDir := env("APPDECK_DATA_DIR", "./data")
	databasePath := filepath.Join(dataDir, "appdeck.db")
	appStore, err := store.Open(ctx, databasePath)
	if err != nil {
		logger.Error("open AppDeck store", "error", err)
		os.Exit(1)
	}
	defer appStore.Close()

	demoEnabled := envBool("APPDECK_DEMO_AGENT", true)
	if demoEnabled {
		if err := appStore.SeedDemo(ctx); err != nil {
			logger.Error("seed demo workspace", "error", err)
			os.Exit(1)
		}
		go demo.NewAgent(appStore, logger).Run(ctx)
	}
	go reapExpiredLeases(ctx, appStore, logger)

	httpOptions := make([]httpapi.Option, 0, 3)
	serviceAuthFile := strings.TrimSpace(os.Getenv("APPDECK_SERVICE_AUTH_FILE"))
	if serviceAuthFile != "" {
		authorizer, err := httpapi.NewFileServiceAuthorizer(serviceAuthFile)
		if err != nil {
			logger.Error("load service authorization policy", "error", err)
			os.Exit(1)
		}
		httpOptions = append(httpOptions, httpapi.WithServiceAuthorizer(authorizer))
	}
	operatorAuthFile := strings.TrimSpace(os.Getenv("APPDECK_OPERATOR_AUTH_FILE"))
	if operatorAuthFile != "" {
		authenticator, err := httpapi.NewFileOperatorAuthenticator(operatorAuthFile)
		if err != nil {
			logger.Error("load operator authentication policy", "error", err)
			os.Exit(1)
		}
		httpOptions = append(
			httpOptions,
			httpapi.WithOperatorAuthenticator(authenticator),
			httpapi.WithSecureOperatorCookies(envBool("APPDECK_OPERATOR_COOKIE_SECURE", true)),
		)
	}

	api := httpapi.New(appStore, logger, httpOptions...).Handler()
	mux := http.NewServeMux()
	mux.Handle("/api/", api)
	mux.Handle("/healthz", api)
	mux.Handle("/readyz", api)
	mux.Handle("/", webui.Handler())

	server := &http.Server{
		Addr:              env("APPDECK_ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info(
			"AppDeck is ready",
			"address", server.Addr,
			"database", databasePath,
			"demo_agent", demoEnabled,
			"service_auth", serviceAuthFile != "",
			"operator_auth", operatorAuthFile != "",
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve AppDeck", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		logger.Error("shutdown AppDeck", "error", err)
	}
}

func reapExpiredLeases(ctx context.Context, appStore *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := appStore.ExpireLeases(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				logger.Warn("expire agent leases", "error", err)
			} else if count > 0 {
				logger.Info("expired agent leases", "count", count)
			}
		}
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
