IMAGE ?= appdeck:dev

.PHONY: setup web build run dev-api dev-web check test image manifests clean

setup:
	npm --prefix web install

web:
	npm --prefix web run build

build: web
	mkdir -p bin
	go build -trimpath -o bin/appdeck ./cmd/appdeck

run: build
	./bin/appdeck

dev-api:
	APPDECK_LOG_LEVEL=debug go run ./cmd/appdeck

dev-web:
	npm --prefix web run dev

check:
	go vet ./...
	npm --prefix web run check

test:
	go test ./...
	npm --prefix web test

image:
	docker build --tag $(IMAGE) .

manifests:
	kustomize build deploy/kubernetes/base

clean:
	go clean
	npm --prefix web run clean --if-present
