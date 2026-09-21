package worker

import (
	"context"
	"errors"
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
