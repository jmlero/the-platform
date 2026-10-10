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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
	if modes := strings.FieldsFunc(os.Getenv("MARINA_FAULT_STREAM"), func(r rune) bool { return r == ',' }); len(modes) > 0 {
		engineDialOptions = append(engineDialOptions, grpc.WithStreamInterceptor(streamFaults(modes)))
	}
	if len(routes) > 0 {
		sourceTransport = &dropTransport{next: http.DefaultTransport, armed: &onceSet{pending: routes}}
	}
	if len(deckRoutes) > 0 {
		deckTransport = &dropTransport{next: http.DefaultTransport, armed: &onceSet{pending: deckRoutes}}
	}
	// Hold only the first real provider response so live tests can revoke
	// authority during an accepted call. Release builds have no delay hook.
	if delay, err := time.ParseDuration(os.Getenv("MARINA_FAULT_MODEL_DELAY")); err == nil && delay > 0 && delay <= 3*time.Minute {
		modelTransport = &delayModelTransport{next: http.DefaultTransport, delay: delay}
	}
}

type delayModelTransport struct {
	next  http.RoundTripper
	delay time.Duration
	mu    sync.Mutex
	used  bool
}

func (t *delayModelTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.next.RoundTrip(request)
	if err != nil || request.URL.Host != "openrouter.ai" || request.URL.Path != "/api/v1/responses" || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, err
	}
	t.mu.Lock()
	delay := !t.used
	t.used = true
	t.mu.Unlock()
	if delay {
		slog.Warn("fault injection delayed an accepted model response")
		response.Body = &delayedModelBody{ReadCloser: response.Body, ctx: request.Context(), delay: t.delay}
	}
	return response, nil
}

type delayedModelBody struct {
	io.ReadCloser
	ctx   context.Context
	delay time.Duration
	once  sync.Once
	err   error
}

func (b *delayedModelBody) Read(buffer []byte) (int, error) {
	b.once.Do(func() {
		timer := time.NewTimer(b.delay)
		defer timer.Stop()
		select {
		case <-b.ctx.Done():
			b.err = b.ctx.Err()
		case <-timer.C:
		}
	})
	if b.err != nil {
		return 0, b.err
	}
	return b.ReadCloser.Read(buffer)
}

// The test-only stream faults change received events, never engine requests.
// Each configured mode is used on one subscription in order.
func streamFaults(modes []string) grpc.StreamClientInterceptor {
	var mu sync.Mutex
	var firstSnapshot proto.Message
	return func(ctx context.Context, desc *grpc.StreamDesc, conn *grpc.ClientConn, method string, streamer grpc.Streamer, options ...grpc.CallOption) (grpc.ClientStream, error) {
		stream, err := streamer(ctx, desc, conn, method, options...)
		if err != nil || !strings.HasSuffix(method, "/SubscribeToTask") {
			return stream, err
		}
		mu.Lock()
		defer mu.Unlock()
		mode := ""
		if len(modes) > 0 {
			mode = modes[0]
			modes = modes[1:]
		}
		return &faultStream{ClientStream: stream, mode: mode, previous: firstSnapshot, capture: func(message proto.Message) {
			mu.Lock()
			defer mu.Unlock()
			if firstSnapshot == nil {
				firstSnapshot = proto.Clone(message)
			}
		}}, nil
	}
}

type faultStream struct {
	grpc.ClientStream
	mode     string
	reads    int
	saved    proto.Message
	previous proto.Message
	capture  func(proto.Message)
}

func (s *faultStream) RecvMsg(message any) error {
	s.reads++
	if s.mode == "disconnect" && s.reads > 1 {
		slog.Warn("fault injection changed an engine stream", "mode", s.mode)
		return status.Error(codes.Unavailable, "fault injection: stream disconnected")
	}
	if s.saved != nil {
		proto.Reset(message.(proto.Message))
		proto.Merge(message.(proto.Message), s.saved)
		s.saved = nil
		slog.Warn("fault injection changed an engine stream", "mode", s.mode)
		return nil
	}
	if err := s.ClientStream.RecvMsg(message); err != nil {
		return err
	}
	if s.reads == 1 && s.capture != nil {
		s.capture(message.(proto.Message))
	}
	if s.reads == 1 && (s.mode == "duplicate" || s.mode == "reorder") {
		s.saved = proto.Clone(message.(proto.Message))
		if s.mode == "reorder" {
			// A bounded subscription often contains just its initial snapshot.
			// Replay an earlier committed snapshot after this fresh one instead
			// of waiting for two events that may outlive the observation window.
			if s.previous != nil {
				s.saved = proto.Clone(s.previous)
				return nil
			}
			proto.Reset(message.(proto.Message))
			if err := s.ClientStream.RecvMsg(message); err != nil {
				return err
			}
		}
	}
	return nil
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
