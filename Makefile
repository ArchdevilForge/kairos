.PHONY: build vet test test-race lint fmt-check mod-check vuln check cover cover-check

GOLANGCI_LINT ?= $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go/bin/golangci-lint)
GOVULNCHECK ?= $(shell command -v govulncheck 2>/dev/null || echo $(HOME)/go/bin/govulncheck)
COVERAGE_MIN ?= 70
COVER_PACKAGES := $(shell go list ./internal/... | grep -v '/types$$')

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

lint:
	$(GOLANGCI_LINT) run ./...

fmt-check:
	@files=$$(gofmt -l cmd internal tests); \
	if [ -n "$$files" ]; then \
		echo "gofmt needed on:"; echo "$$files"; exit 1; \
	fi

mod-check:
	go mod verify
	go mod tidy -diff

vuln:
	$(GOVULNCHECK) ./...

cover:
	go test $(COVER_PACKAGES) -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | tail -1

cover-check: cover
	@pct=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/,""); print $$3}'); \
	echo "internal coverage: $$pct% (min $(COVERAGE_MIN)%)"; \
	awk -v p="$$pct" -v m="$(COVERAGE_MIN)" 'BEGIN { if (p+0 < m+0) { exit 1 } }'

check: build vet fmt-check mod-check lint test-race
