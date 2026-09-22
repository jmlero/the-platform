package worker

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestAuthorityWatchdogSurvivesTemporaryGatewayOutage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	expires := time.Now().Add(time.Second)
	calls := 0
	err := watchAuthority(ctx, expires, 10*time.Millisecond, time.Millisecond, func(context.Context) (time.Time, error) {
		calls++
		if calls == 1 {
			return time.Time{}, gatewayStatusError{503}
		}
		cancel()
		return expires, nil
	})
	if calls != 2 || !errors.Is(err, context.Canceled) {
		t.Fatalf("temporary unavailability killed a valid lease: calls=%d err=%v", calls, err)
	}
}

func TestAuthorityWatchdogStopsImmediatelyOnRevocation(t *testing.T) {
	calls := 0
	err := watchAuthority(context.Background(), time.Now().Add(time.Second), 10*time.Millisecond, time.Millisecond, func(context.Context) (time.Time, error) {
		calls++
		return time.Time{}, gatewayStatusError{403}
	})
	var status gatewayStatusError
	if calls != 1 || !errors.As(err, &status) || status.Code != 403 {
		t.Fatalf("revocation was retried: calls=%d err=%v", calls, err)
	}
}

func TestAuthorityWatchdogNeverOutlivesLastConfirmedLease(t *testing.T) {
	deadline := time.Now().Add(80 * time.Millisecond)
	err := watchAuthority(context.Background(), deadline, 10*time.Millisecond, time.Millisecond, func(ctx context.Context) (time.Time, error) {
		requestDeadline, ok := ctx.Deadline()
		if !ok || requestDeadline.After(deadline.Add(-10*time.Millisecond)) {
			t.Fatal("authority request can outlive the confirmed lease")
		}
		<-ctx.Done()
		return time.Time{}, ctx.Err()
	})
	if err == nil {
		t.Fatal("expired authority was retained")
	}
}

func TestInitialGatewayReadRetriesOnlyTransientFailures(t *testing.T) {
	for _, code := range []int{503, 403} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			calls := 0
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			value, err := retryGatewayRead(ctx, time.Millisecond, func(context.Context) ([]byte, error) {
				calls++
				if calls == 1 {
					return nil, gatewayStatusError{code}
				}
				return []byte("frozen input"), nil
			})
			if code == 503 && (err != nil || calls != 2 || string(value) != "frozen input") {
				t.Fatalf("transient startup: calls=%d err=%v", calls, err)
			}
			if code == 403 && (err == nil || calls != 1) {
				t.Fatalf("revocation retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestInitialGatewayReadStopsAtDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := retryGatewayRead(ctx, time.Millisecond, func(context.Context) ([]byte, error) { return nil, gatewayStatusError{503} })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded startup: %v", err)
	}
}
