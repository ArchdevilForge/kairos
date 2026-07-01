.PHONY: build vet test test-race lint check

GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	$(GOLANGCI_LINT) run ./...

check: build vet lint test-race
