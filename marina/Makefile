IMAGE ?= marina:dev
VERSION ?= dev

.PHONY: build check conformance-deck fmt image manifests run test vet

build:
	go build -o bin/marina ./cmd/marina

check: test vet

conformance-deck:
	DECK_REPO="$(DECK_REPO)" ./scripts/test-deck-conformance.sh

fmt:
	gofmt -w cmd internal

image:
	docker build --build-arg VERSION=$(VERSION) --tag $(IMAGE) .

manifests:
	kustomize build deploy/kubernetes/base

run:
	go run ./cmd/marina serve

test:
	go test ./...

vet:
	go vet ./...
