//go:build sandbox

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jmlero/the-platform/marina/internal/kubeapi"
	"github.com/jmlero/the-platform/marina/internal/platformapi/sandbox"
)

// api-sandbox serves the real platform API with an in-memory Kubernetes API
// and a simulated executor, for client contract tests (MAR-07). Only builds
// with -tags sandbox include it.
func init() {
	testCommands["api-sandbox"] = func(arguments []string, stdout, stderr io.Writer) int {
		flags := flag.NewFlagSet("api-sandbox", flag.ContinueOnError)
		flags.SetOutput(stderr)
		address := flags.String("addr", "127.0.0.1:18090", "listen address (loopback only)")
		dir := flags.String("dir", "", "directory for the sandbox journal and artifacts (default: a new temporary directory)")
		var kube kubeapi.Config
		flags.StringVar(&kube.ServerURL, "kube-server", "", "use this Kubernetes API server instead of the in-memory one")
		flags.StringVar(&kube.TokenFile, "kube-token-file", "", "service account token for --kube-server")
		flags.StringVar(&kube.CAFile, "kube-ca-file", "", "CA bundle for --kube-server")
		if err := flags.Parse(arguments); err != nil {
			return 2
		}
		if host, _, err := net.SplitHostPort(*address); err != nil || (host != "127.0.0.1" && host != "localhost") {
			_, _ = fmt.Fprintln(stderr, "the sandbox listens on loopback only")
			return 2
		}
		if *dir == "" {
			temporary, err := os.MkdirTemp("", "marina-sandbox-")
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err)
				return 1
			}
			defer os.RemoveAll(temporary)
			*dir = temporary
		}
		listener, err := net.Listen("tcp", *address)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		logger := slog.New(slog.NewJSONHandler(stderr, nil))
		var s *sandbox.Sandbox
		if kube.ServerURL == "" {
			s, err = sandbox.Start(ctx, listener, filepath.Join(*dir, "marina.db"), filepath.Join(*dir, "artifacts"), 100*time.Millisecond, logger)
		} else {
			var client *kubeapi.Client
			if client, err = kubeapi.New(kube, nil); err == nil {
				s, err = sandbox.StartWith(ctx, listener, client, filepath.Join(*dir, "marina.db"), filepath.Join(*dir, "artifacts"), 100*time.Millisecond, logger)
			}
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "Marina API sandbox at http://%s (namespace %s, project %s; tokens user:<name>)\n", listener.Addr(), sandbox.Namespace, sandbox.Project)
		<-ctx.Done()
		if err := s.Close(); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
}
