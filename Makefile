# oelala-storage Makefile

BINARY_NAME=oelala-storage
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -s -w"

.PHONY: all build build-all build-linux build-windows build-android clean test lint

all: build

# Development build
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/oelala-storage

# Build for all platforms
build-all: build-linux build-windows build-android

# Linux builds
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/oelala-storage
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/oelala-storage

# Windows builds
build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/oelala-storage

# Android build (arm64)
build-android:
	GOOS=android GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-android-arm64 ./cmd/oelala-storage

# Run locally
run:
	go run ./cmd/oelala-storage serve

# Run with hot reload (requires air)
dev:
	air

# Run tests
test:
	go test -v -race ./...

# Run tests with coverage
test-coverage:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run

# Format code
fmt:
	go fmt ./...
	gofumpt -l -w .

# Download dependencies
deps:
	go mod download
	go mod tidy

# Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Generate protobuf (requires protoc)
proto:
	protoc --go_out=. --go-grpc_out=. proto/*.proto

# Install dev tools
install-tools:
	go install github.com/cosmtrek/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install mvdan.cc/gofumpt@latest
