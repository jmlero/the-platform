package main

import (
	"fmt"
	"os"

	"github.com/jmlero/the-platform/marina/internal/analysisbyo"
)

func main() {
	if err := analysisbyo.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "analysis BYO actor stopped")
		os.Exit(1)
	}
}
