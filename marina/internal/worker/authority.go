package worker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type gatewayStatusError struct{ Code int }

func (err gatewayStatusError) Error() string {
	return fmt.Sprintf("worker gateway returned HTTP %d", err.Code)
}

// A temporary outage does not revoke a previously confirmed lease. Gateway
// operations still require live authority; local work stops before that lease
// expires, or immediately when the gateway explicitly rejects the credential.
func watchAuthority(ctx context.Context, expires time.Time, safety, interval time.Duration, check func(context.Context) (time.Time, error)) error {
	for {
		deadline := expires.Add(-safety)
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("worker authority expired")
		}
		timer := time.NewTimer(min(interval, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		checkCtx, cancel := context.WithDeadline(ctx, deadline)
		next, err := check(checkCtx)
		cancel()
		if err != nil {
			var status gatewayStatusError
			var network net.Error
			if (errors.As(err, &status) && status.Code >= 500) || errors.As(err, &network) {
				continue
			}
			return err
		}
		expires = next
	}
}
