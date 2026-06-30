.PHONY: all build clean lint test run

BINARY=kairosd

all: lint build test

build:
	go build -o bin/$(BINARY) ./cmd/kairosd

lint:
	golangci-lint run ./...

test:
	go test -race ./...

run:
	go run ./cmd/kairosd

clean:
	rm -rf bin/
