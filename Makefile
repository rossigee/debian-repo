.PHONY: help build test lint fmt vet docker docker-push clean

VERSION ?= $(shell cat VERSION)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Version=$(VERSION) \
           -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Commit=$(COMMIT) \
           -X git.golder.lan/rossgolderltd/debian-repo/internal/version.Date=$(DATE)

IMAGE ?= ghcr.io/rossigee/debian-repo
DOCKER_TAG ?= $(VERSION)

help:
	@echo "debian-repo - Golang Debian package repository service"
	@echo ""
	@echo "Targets:"
	@echo "  build         Build debian-repo and repoctl binaries"
	@echo "  test          Run tests with race detector and coverage"
	@echo "  lint          Run golangci-lint"
	@echo "  fmt           Format code with gofmt"
	@echo "  vet           Run go vet"
	@echo "  docker        Build Docker image"
	@echo "  docker-push   Push Docker image to registry"
	@echo "  clean         Remove build artifacts"

build: bin/debian-repo bin/repoctl

bin/debian-repo: $(shell find . -name '*.go' -not -path './vendor/*')
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/debian-repo ./cmd/debian-repo

bin/repoctl: $(shell find . -name '*.go' -not -path './vendor/*')
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/repoctl ./cmd/repoctl

test:
	go test -race -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	golangci-lint run ./... --config .golangci.yml --timeout=5m

fmt:
	go fmt ./...

vet:
	go vet ./...

docker: build
	docker build -t $(IMAGE):$(DOCKER_TAG) -t $(IMAGE):latest \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) .

docker-push: docker
	docker push $(IMAGE):$(DOCKER_TAG)
	docker push $(IMAGE):latest

clean:
	rm -rf bin/ coverage.out coverage.html
	go clean ./...
