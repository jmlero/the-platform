VERSION ?= dev
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
DIST_DIR ?= $(CURDIR)/dist
PACKAGE_VERSION := $(patsubst v%,%,$(VERSION))

.PHONY: build check clean fmt package package-test test vet

build:
	mkdir -p bin
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/platformctl ./cmd/platformctl

check: test vet package-test

clean:
	go clean

fmt:
	gofmt -w cmd internal

package:
	@printf '%s\n' "$(VERSION)" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$$' || (echo "VERSION must be a semantic version such as v1.2.3" >&2; exit 1)
	@mkdir -p "$(abspath $(DIST_DIR))"
	@package_dir=$$(mktemp -d); \
	trap 'rm -rf "$$package_dir"' EXIT HUP INT TERM; \
	binary=platformctl; \
	if [ "$(GOOS)" = "windows" ]; then binary=platformctl.exe; fi; \
	CGO_ENABLED=0 GOOS="$(GOOS)" GOARCH="$(GOARCH)" go build -trimpath \
		-ldflags "-s -w -X main.version=$(VERSION)" \
		-o "$$package_dir/$$binary" ./cmd/platformctl; \
	archive="$(abspath $(DIST_DIR))/platformctl_$(PACKAGE_VERSION)_$(GOOS)_$(GOARCH)"; \
	if [ "$(GOOS)" = "windows" ]; then \
		(cd "$$package_dir" && zip -q "$$archive.zip" "$$binary"); \
	else \
		tar -C "$$package_dir" -czf "$$archive.tar.gz" "$$binary"; \
	fi

package-test:
	sh scripts/test-package.sh

test:
	go test ./...

vet:
	go vet ./...
