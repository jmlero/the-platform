//go:build faultinject

package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFaultInjectionDropsOneCommittedResponsePerTarget(t *testing.T) {
	calls := 0
	interceptor := dropUnaryOnce([]string{"/svc/Create"})
	invoke := func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
		calls++
		return nil
	}
	for i, want := range []bool{true, false} {
		err := interceptor(context.Background(), "/svc/Create", nil, nil, nil, invoke)
		if (err != nil) != want {
			t.Fatalf("create call %d: err=%v", i, err)
		}
	}
	if err := interceptor(context.Background(), "/svc/Get", nil, nil, nil, invoke); err != nil || calls != 3 {
		t.Fatalf("unarmed method: %v, upstream calls %d", err, calls)
	}
	second := dropUnaryOnce([]string{"/svc/Send#2"})
	for i, want := range []bool{false, true, false} {
		if err := second(context.Background(), "/svc/Send", nil, nil, nil, invoke); (err != nil) != want {
			t.Fatalf("send call %d: err=%v", i+1, err)
		}
	}

	upstream := 0
	transport := &dropTransport{armed: &onceSet{pending: []string{"PATCH /git/refs/heads/main"}}, next: roundTrip(func(*http.Request) (*http.Response, error) {
		upstream++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	request := func(method, path string) error {
		r, _ := http.NewRequest(method, "https://api.github.com"+path, nil)
		_, err := transport.RoundTrip(r)
		return err
	}
	if request("PATCH", "/repos/o/r/git/refs/heads/main") == nil {
		t.Fatal("armed route was not dropped")
	}
	if request("PATCH", "/repos/o/r/git/refs/heads/main") != nil || request("GET", "/repos/o/r/git/ref/heads/main") != nil || upstream != 3 {
		t.Fatalf("route dropped more than once or blocked an unarmed route; upstream=%d", upstream)
	}
}

type numberedStream struct {
	grpc.ClientStream
	next int32
}

func (s *numberedStream) RecvMsg(message any) error {
	s.next++
	if s.next > 3 {
		return io.EOF
	}
	proto.Merge(message.(proto.Message), wrapperspb.Int32(s.next))
	return nil
}

func TestStreamFaultsAffectDeliveryWithoutResending(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want []int32
	}{
		{"duplicate", []int32{1, 1, 2, 3}},
		{"reorder", []int32{2, 1, 3}},
		{"disconnect", []int32{1}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			stream := &faultStream{ClientStream: &numberedStream{}, mode: tc.mode}
			for _, want := range tc.want {
				message := &wrapperspb.Int32Value{}
				if err := stream.RecvMsg(message); err != nil || message.Value != want {
					t.Fatalf("got %v %v, want %d", message, err, want)
				}
			}
			err := stream.RecvMsg(&wrapperspb.Int32Value{})
			if tc.mode == "disconnect" {
				if status.Code(err) != codes.Unavailable {
					t.Fatalf("disconnect: %v", err)
				}
			} else if err != io.EOF {
				t.Fatalf("stream tail: %v", err)
			}
		})
	}
}

func TestModelDelayHoldsOneAcceptedResponseAndRespectsRevocation(t *testing.T) {
	calls := 0
	transport := &delayModelTransport{delay: time.Minute, next: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("accepted"))}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	request, _ := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/responses", nil)
	response, err := transport.RoundTrip(request)
	if err != nil || calls != 1 {
		t.Fatalf("upstream was not accepted once: %v, %d", err, calls)
	}
	cancel()
	if _, err := response.Body.Read(make([]byte, 10)); err != context.Canceled {
		t.Fatalf("revocation did not stop delayed delivery: %v", err)
	}
	_ = response.Body.Close()
	request, _ = http.NewRequest("POST", "https://openrouter.ai/api/v1/responses", nil)
	response, err = transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if body, err := io.ReadAll(response.Body); err != nil || string(body) != "accepted" || calls != 2 {
		t.Fatalf("unarmed response changed: %q %v, calls=%d", body, err, calls)
	}
}

func TestStreamReorderingCanReplayAnOlderCommittedSnapshotAcrossSubscriptions(t *testing.T) {
	stream := &faultStream{ClientStream: &numberedStream{}, mode: "reorder", previous: wrapperspb.Int32(0)}
	for _, want := range []int32{1, 0, 2} {
		message := &wrapperspb.Int32Value{}
		if err := stream.RecvMsg(message); err != nil || message.Value != want {
			t.Fatalf("got %v %v, want %d", message, err, want)
		}
	}
}
