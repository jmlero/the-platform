//go:build faultinject

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MARINA_FAULTS lists responses this process drops once, after the upstream
// accepted the request, for live lost-response acceptance, for example:
// "grpc:/kagent.api.v1alpha1.AgentInstanceService/CreateAgentInstance,http:PATCH /git/refs/heads/main".
// "http:" targets the Git provider and "deck:" the work-control provider.
func init() {
	var methods, routes, deckRoutes []string
	for _, item := range strings.Split(os.Getenv("MARINA_FAULTS"), ",") {
		item = strings.TrimSpace(item)
		if method, ok := strings.CutPrefix(item, "grpc:"); ok {
			methods = append(methods, method)
		} else if route, ok := strings.CutPrefix(item, "http:"); ok {
			routes = append(routes, route)
		} else if route, ok := strings.CutPrefix(item, "deck:"); ok {
			deckRoutes = append(deckRoutes, route)
		}
	}
	if len(methods) > 0 {
		engineDialOptions = append(engineDialOptions, grpc.WithUnaryInterceptor(dropUnaryOnce(methods)))
	}
	if len(routes) > 0 {
		sourceTransport = &dropTransport{next: http.DefaultTransport, armed: &onceSet{pending: routes}}
	}
	if len(deckRoutes) > 0 {
		deckTransport = &dropTransport{next: http.DefaultTransport, armed: &onceSet{pending: deckRoutes}}
	}
}

// onceSet holds armed targets. "target#N" drops only the Nth matching call.
type onceSet struct {
	mu      sync.Mutex
	pending []string
	seen    map[string]int
}

// take reports whether key is still armed and disarms it.
func (s *onceSet) take(key string, matches func(armed, key string) bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == nil {
		s.seen = map[string]int{}
	}
	for i, armed := range s.pending {
		target, nth := armed, 1
		if before, after, ok := strings.Cut(armed, "#"); ok {
			if n, err := strconv.Atoi(after); err == nil && n > 0 {
				target, nth = before, n
			}
		}
		if !matches(target, key) {
			continue
		}
		s.seen[armed]++
		if s.seen[armed] == nth {
			s.pending = append(s.pending[:i], s.pending[i+1:]...)
			return true
		}
		return false
	}
	return false
}

func dropUnaryOnce(methods []string) grpc.UnaryClientInterceptor {
	armed := &onceSet{pending: methods}
	return func(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		err := invoke(ctx, method, request, reply, conn, options...)
		if err == nil && armed.take(method, func(a, k string) bool { return a == k }) {
			slog.Warn("fault injection dropped a committed engine response", "method", method)
			return status.Error(codes.Unavailable, "fault injection: response dropped")
		}
		return err
	}
}

type dropTransport struct {
	next  http.RoundTripper
	armed *onceSet
}

// A route is "METHOD path-suffix", matched against the request URL path.
func (t *dropTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil || response.StatusCode < 200 || response.StatusCode > 299 {
		return response, err
	}
	route := request.Method + " " + request.URL.Path
	if t.armed.take(route, func(armed, key string) bool {
		method, suffix, _ := strings.Cut(armed, " ")
		return strings.HasPrefix(key, method+" ") && strings.HasSuffix(key, suffix)
	}) {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		slog.Warn("fault injection dropped a committed source response", "route", route)
		return nil, errors.New("fault injection: response dropped")
	}
	return response, nil
}
