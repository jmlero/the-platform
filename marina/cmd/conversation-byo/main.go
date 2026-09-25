package main

import (
	"fmt"
	"os"

	"github.com/jmlero/the-platform/marina/internal/conversationbyo"
)

func main() {
	if err := conversationbyo.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, "conversation BYO actor stopped")
		os.Exit(1)
	}
}
