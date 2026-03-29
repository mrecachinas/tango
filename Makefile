BINARY := tango
GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')

.PHONY: all build fmt vet lint test clean run

all: fmt vet lint build

build:
	go build -o $(BINARY) ./cmd/tango

fmt:
	gofmt -s -w $(GOFILES)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -v -race ./...

clean:
	rm -f $(BINARY)
	go clean

run: build
	./$(BINARY)

## ── example environment ──

.PHONY: example-up example-down example-seed example-run

example-up:
	docker compose -f example/docker-compose.yml up -d

example-down:
	docker compose -f example/docker-compose.yml down -v

example-seed: example-up
	@echo "Waiting for MongoDB to be ready..."
	@sleep 3
	docker compose -f example/docker-compose.yml exec -T mongo \
		mongosh --quiet --file /seed/seed.js

example-run: example-seed build
	./$(BINARY) --mongo-host localhost --mongo-port 27017
