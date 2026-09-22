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

func transientGatewayError(err error) bool {
	var status gatewayStatusError
	var network net.Error
	return (errors.As(err, &status) && status.Code >= 500) || errors.As(err, &network)
}

// Startup performs only authenticated reads until authority and frozen input
// are available. A controller restart may briefly interrupt either read.
func retryGatewayRead(ctx context.Context, interval time.Duration, read func(context.Context) ([]byte, error)) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := read(ctx)
		if err == nil || !transientGatewayError(err) {
			return value, err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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
			if transientGatewayError(err) {
				continue
			}
			return err
		}
		expires = next
	}
}
