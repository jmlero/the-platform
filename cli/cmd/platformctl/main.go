package main

import (
	"context"
	"os"

	"github.com/jmlero/the-platform/cli/internal/command"
)

var version = "dev"

func main() {
	os.Exit(command.Run(
		context.Background(),
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		os.Getenv,
		version,
	))
}
