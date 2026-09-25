IMAGE ?= deck:dev

.PHONY: setup web build run dev-api dev-web check test conformance image manifests clean

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

conformance:
	python3 scripts/test-threads.py
	python3 scripts/test-project-work.py

image:
	docker build --tag $(IMAGE) .

manifests:
	kustomize build deploy/kubernetes/base

clean:
	go clean
	npm --prefix web run clean --if-present
