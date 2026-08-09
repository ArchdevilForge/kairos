.PHONY: build vet test test-race lint fmt-check mod-check vuln check check-python check-all cover cover-check cross-check

# Python 子项目(monorepo): 每个有 pyproject.toml 的目录
PYTHON_PROJECTS := bus coinglass floors/meme floors/pm floors/solana floors/onchain data-sources/aicoin tools/chain-trace

# Platforms the release workflow ships. Keep in sync with the matrix in
# .github/workflows/release.yml.
RELEASE_PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

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

# cross-check builds every command in ./cmd/ for every release target. The
# release job discovers commands the same way, so this fails at PR time if a
# new command cannot be packaged — instead of at tag time, when it is too late.
cross-check:
	@set -e; \
	for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		for dir in ./cmd/*/; do \
			cmd=$$(basename $$dir); \
			GOOS=$$os GOARCH=$$arch go build -trimpath -o /dev/null $$dir \
				|| { echo "FAIL: $$cmd for $$os/$$arch"; exit 1; }; \
		done; \
		echo "  ok $$os/$$arch ($$(ls -d ./cmd/*/ | wc -l) commands)"; \
	done

cover:
	go test $(COVER_PACKAGES) -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | tail -1

cover-check: cover
	@pct=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/,""); print $$3}'); \
	echo "internal coverage: $$pct% (min $(COVERAGE_MIN)%)"; \
	awk -v p="$$pct" -v m="$(COVERAGE_MIN)" 'BEGIN { if (p+0 < m+0) { exit 1 } }'

# 每个 Python 子项目: uv sync(含 dev 组) + pytest。CI 与本地共用。
check-python:
	@set -e; for d in $(PYTHON_PROJECTS); do \
		echo "== $$d"; \
		(cd $$d && uv sync --quiet && (uv run pytest -q || test $$? -eq 5)); \
	done

check-all: check check-python

check: build vet fmt-check mod-check lint test-race
