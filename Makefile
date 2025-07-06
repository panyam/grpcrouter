.PHONY: proto build test clean install-deps run-example

# Variables
PROTO_DIR := proto
EXAMPLE_DIR := examples/myservice
GO_FILES := $(shell find . -name "*.go" -not -path "./vendor/*")
PROTO_FILES := $(shell find $(PROTO_DIR) -name "*.proto")
EXAMPLE_PROTO_FILES := $(shell find $(EXAMPLE_DIR) -name "*.proto")

# Default target
all: proto build

# Install dependencies
install-deps:
	go mod download
	go mod tidy

# Generate protobuf code
proto: $(PROTO_FILES) $(EXAMPLE_PROTO_FILES)
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		$(EXAMPLE_PROTO_FILES)

# Build the project
build: proto
	go build -v ./...

# Run tests
test: proto
	go test -v ./...

# Run tests with coverage
test-coverage: proto
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean generated files
clean:
	find . -name "*.pb.go" -delete
	find . -name "*_grpc.pb.go" -delete
	rm -f coverage.out coverage.html

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run

# Run example service
run-example: build
	go run examples/myservice/main.go

# Run router service
run-router: build
	go run cmd/router/main.go

# Build router binary
build-router: proto
	go build -o bin/grpcrouter cmd/router/main.go

# Build example service binary
build-example: proto
	go build -o bin/myservice examples/myservice/main.go

# Install protoc plugin
install-plugin: proto
	go build -o bin/protoc-gen-grpcrouter plugin/protoc-gen-grpcrouter/main.go
	cp bin/protoc-gen-grpcrouter $(GOPATH)/bin/

# Development workflow
dev: clean proto build test

# Help target
help:
	@echo "Available targets:"
	@echo "  all           - Generate protobuf code and build"
	@echo "  proto         - Generate protobuf Go code"
	@echo "  build         - Build the project"
	@echo "  test          - Run tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  clean         - Clean generated files"
	@echo "  fmt           - Format Go code"
	@echo "  lint          - Run linter"
	@echo "  run-example   - Run example service"
	@echo "  run-router    - Run router service"
	@echo "  build-router  - Build router binary"
	@echo "  build-example - Build example service binary"
	@echo "  install-plugin- Install protoc plugin"
	@echo "  dev           - Development workflow (clean, proto, build, test)"
	@echo "  help          - Show this help message"