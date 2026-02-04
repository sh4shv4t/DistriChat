.PHONY: all build run test clean proto deps fmt lint help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Binary name
BINARY_NAME=distribchat
BINARY_PATH=bin/$(BINARY_NAME)

# Build info
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Default target
all: deps build

## help: Show this help message
help:
	@echo "DistriChat - Distributed Chat Routing Engine"
	@echo ""
	@echo "Usage:"
	@echo "  make <target>"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed -e 's/## /  /'
	@echo ""

## deps: Download and tidy dependencies
deps:
	@echo "📦 Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "✅ Dependencies ready"

## build: Build the application
build:
	@echo "🔨 Building DistriChat..."
	@mkdir -p bin
	$(GOBUILD) -o $(BINARY_PATH) -ldflags "-X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)" .
	@echo "✅ Built: $(BINARY_PATH)"

## run: Run the simulation directly
run:
	@echo "🚀 Starting DistriChat simulation..."
	$(GORUN) main.go

## run-binary: Build and run the compiled binary
run-binary: build
	@echo "🚀 Running compiled binary..."
	./$(BINARY_PATH)

## test: Run all tests
test:
	@echo "🧪 Running tests..."
	$(GOTEST) -v ./...
	@echo "✅ Tests complete"

## test-cover: Run tests with coverage
test-cover:
	@echo "🧪 Running tests with coverage..."
	$(GOTEST) -v -cover -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

## test-ring: Run only hash ring tests
test-ring:
	@echo "🧪 Testing hash ring..."
	$(GOTEST) -v ./pkg/ring/...

## test-cache: Run only cache tests
test-cache:
	@echo "🧪 Testing cache..."
	$(GOTEST) -v ./pkg/cache/...

## bench: Run benchmarks
bench:
	@echo "📊 Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

## fmt: Format code
fmt:
	@echo "🎨 Formatting code..."
	$(GOFMT) -s -w .
	@echo "✅ Code formatted"

## lint: Run linters (requires golangci-lint)
lint:
	@echo "🔍 Linting code..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...
	@echo "✅ Linting complete"

## proto: Generate protobuf code (requires protoc)
proto:
	@echo "📝 Generating protobuf code..."
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/chat.proto
	@echo "✅ Protobuf code generated"

## clean: Clean build artifacts
clean:
	@echo "🧹 Cleaning..."
	$(GOCLEAN)
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "✅ Clean complete"

## docker-build: Build Docker image
docker-build:
	@echo "🐳 Building Docker image..."
	docker build -t distribchat:latest .
	@echo "✅ Docker image built"

## docker-run: Run in Docker
docker-run: docker-build
	@echo "🐳 Running in Docker..."
	docker run --rm distribchat:latest

# Development helpers
## watch: Run with file watching (requires air)
watch:
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

## install-tools: Install development tools
install-tools:
	@echo "🔧 Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/cosmtrek/air@latest
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "✅ Tools installed"
