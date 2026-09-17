APPDECK_REPO ?= $(CURDIR)/deck

.PHONY: build check check-installation conformance fmt test vet

build:
	$(MAKE) -C cli build
	$(MAKE) -C deck build
	$(MAKE) -C marina build

check: check-installation
	$(MAKE) -C cli check
	$(MAKE) -C deck test check
	$(MAKE) -C marina check

check-installation:
	python3 -m unittest discover -s scripts -p 'test_*.py'

conformance:
	$(MAKE) -C cli conformance-appdeck APPDECK_REPO="$(APPDECK_REPO)"
	$(MAKE) -C marina conformance-appdeck APPDECK_REPO="$(APPDECK_REPO)"

fmt:
	$(MAKE) -C cli fmt
	gofmt -w deck/cmd deck/internal
	$(MAKE) -C marina fmt

test:
	$(MAKE) -C cli test
	$(MAKE) -C deck test
	$(MAKE) -C marina test

vet:
	$(MAKE) -C cli vet
	cd deck && go vet ./...
	$(MAKE) -C marina vet
