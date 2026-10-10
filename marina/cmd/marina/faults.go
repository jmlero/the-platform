package main

import (
	"net/http"
	"time"

	"google.golang.org/grpc"
)

// Fault-injection test builds (tag faultinject) set these to drop selected
// responses after the upstream committed the request. Release builds leave
// them empty, so repository work uses the plain engine and Git clients.
var (
	engineDialOptions []grpc.DialOption
	sourceTransport   http.RoundTripper
	deckTransport     http.RoundTripper
	modelTransport    http.RoundTripper
)

func sourceClient() *http.Client {
	if sourceTransport == nil {
		return nil
	}
	return &http.Client{Transport: sourceTransport, Timeout: 15 * time.Second}
}

func modelClient() *http.Client {
	if modelTransport == nil {
		return nil
	}
	return &http.Client{Transport: modelTransport, Timeout: 10 * time.Minute}
}
