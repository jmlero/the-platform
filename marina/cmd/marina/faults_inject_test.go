//go:build faultinject

package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc"
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
