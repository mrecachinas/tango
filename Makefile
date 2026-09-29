BINARY := tango
GOFILES := $(shell find . -name '*.go' -not -path './vendor/*')
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -ldflags "-s -w -X tango/internal/version.Version=$(VERSION) -X tango/internal/version.Commit=$(COMMIT)"

.PHONY: all build fmt vet lint test test-e2e clean run docker

all: fmt vet lint build

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/tango

fmt:
	gofmt -s -w $(GOFILES)

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -v -race ./...

E2E_CONTAINER := tango-e2e-mongo
E2E_PORT ?= 27027

test-e2e:
	docker run -d --rm --name $(E2E_CONTAINER) -p $(E2E_PORT):27017 mongo:7 --replSet rs0
	@until docker exec $(E2E_CONTAINER) mongosh --quiet --eval 'rs.initiate({_id:"rs0",members:[{_id:0,host:"localhost:27017"}]}).ok || rs.status().ok' >/dev/null 2>&1; do sleep 1; done
	TANGO_E2E_MONGO_URI='mongodb://localhost:$(E2E_PORT)/?directConnection=true' go test -race -count=1 ./internal/e2e/; \
		status=$$?; docker stop $(E2E_CONTAINER) >/dev/null; exit $$status

clean:
	rm -f $(BINARY)
	go clean

run: build
	./$(BINARY)

docker:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(BINARY):$(VERSION) .

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
