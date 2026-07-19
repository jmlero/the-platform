APPDECK_REPO ?= $(CURDIR)/deck

.PHONY: build check conformance fmt test vet

build:
	$(MAKE) -C deck build
	$(MAKE) -C marina build

check:
	$(MAKE) -C deck test check
	$(MAKE) -C marina check

conformance:
	$(MAKE) -C marina conformance-appdeck APPDECK_REPO="$(APPDECK_REPO)"

fmt:
	gofmt -w deck/cmd deck/internal
	$(MAKE) -C marina fmt

test:
	$(MAKE) -C deck test
	$(MAKE) -C marina test

vet:
	cd deck && go vet ./...
	$(MAKE) -C marina vet
