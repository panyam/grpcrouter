# Simplified gRPC router generation
.PHONY: generate clean build router service demo all

# Default target
all: generate build

# Clean generated files
clean:
	rm -rf gen/
	rm -f bin/*

# 2-step generation using single buf.gen.yaml + script
generate: clean
	./generate.sh

# Quick generation using buf only (Step 1 only - no router proto Go generation)
generate-quick:
	buf generate

# Build all binaries
build: 
	@echo "Building binaries..."
	go build -o bin/router ./cmd/router
	go build -o bin/service ./cmd/service  
	go build -o bin/demo ./cmd/demo
	@echo "Built: bin/router, bin/service, bin/demo"

# Individual build targets
router: 
	go build -o bin/router ./cmd/router

service:
	go build -o bin/service ./cmd/service

demo:
	go build -o bin/demo ./cmd/demo

# Install protoc plugins (for development)
install-plugins:
	go install ./cmd/protoc-gen-grpcrouter-proto