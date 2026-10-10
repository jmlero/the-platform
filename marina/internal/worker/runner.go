// Package worker runs deterministic verification without an agent harness.
package worker

import "context"

// Run accepts only the independently verified repository stage.
func Run(ctx context.Context, input Input, root string) (Result, error) {
	return RunVerification(ctx, input, root)
}
