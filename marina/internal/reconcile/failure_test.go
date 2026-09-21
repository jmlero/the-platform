package reconcile

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func TestProviderConnectionFailureUsesBoundedRetry(t *testing.T) {
	for _, cause := range []error{syscall.ECONNREFUSED, syscall.ECONNRESET} {
		err := fmt.Errorf("verify provider compatibility: %w", &url.Error{
			Op: "Get", URL: "https://deck.local/api/v1/provider",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: cause},
		})
		if got := classifyExternal(err); got != execution.FailureTransient {
			t.Fatalf("connection failure should preserve recovery: %v classified %s", cause, got)
		}
	}
	if got := classifyExternal(ErrExternalOwnershipMismatch); got != execution.FailurePermanent {
		t.Fatalf("ownership loss must remain permanent: %s", got)
	}
	if got := classifyExternal(errors.New("invalid provider response")); got != execution.FailurePermanent {
		t.Fatalf("invalid response must remain permanent: %s", got)
	}
}
