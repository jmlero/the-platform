IMAGE ?= deck:dev
MARINA_REPO ?= ../marina

.PHONY: setup web build run dev-api dev-web check test browser-test conformance image manifests clean

setup:
	npm --prefix web install

web:
	npm --prefix web run build

build: web
	mkdir -p bin
	go build -trimpath -o bin/deck ./cmd/deck

run: build
	./bin/deck

dev-api:
	DECK_LOG_LEVEL=debug go run ./cmd/deck

dev-web:
	npm --prefix web run dev

check:
	go vet ./...
	npm --prefix web run check

test:
	go test ./...
	npm --prefix web test

browser-test:
	MARINA_REPO="$(abspath $(MARINA_REPO))" npm --prefix web run test:e2e

conformance:
	MARINA_REPO="$(MARINA_REPO)" ./scripts/test-marina.sh

image:
	docker build --tag $(IMAGE) .

manifests:
	kustomize build deploy/kubernetes/base

clean:
	go clean
	npm --prefix web run clean --if-present
