DECK_REPO ?= $(CURDIR)/deck
MARINA_REPO ?= $(CURDIR)/marina

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
	$(MAKE) -C deck conformance
	$(MAKE) -C cli conformance-deck DECK_REPO="$(DECK_REPO)"
	$(MAKE) -C cli conformance-marina MARINA_REPO="$(MARINA_REPO)"
	cd marina && go test -count=1 ./internal/platformapi/...

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
