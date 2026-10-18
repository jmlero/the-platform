IMAGE ?= marina:dev
VERSION ?= dev

.PHONY: build check fmt image manifests run test vet

build:
	go build -o bin/marina ./cmd/marina

check: test vet

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
