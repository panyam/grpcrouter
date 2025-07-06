# gRPC Router Plugin

A gRPC plugin that enables services to act as routers to other gRPC services using bidirectional streaming over persistent connections. This plugin generates service-specific, type-safe router implementations that maintain the original gRPC API while enabling transparent routing.

## Overview

The gRPC Router Plugin solves the problem of routing gRPC requests through firewalls and network boundaries by:

1. **Service instances connect to a gateway** using persistent bidirectional streams
2. **Clients call the gateway** using normal gRPC calls
3. **Gateway routes requests** to appropriate service instances via the persistent streams
4. **Responses flow back** through the same streams to the original clients

## Key Features

- **Type-safe routing**: Generated service-specific routers with compile-time type checking
- **All gRPC method types supported**: Unary, server streaming, client streaming, and bidirectional streaming
- **Zero buffering**: Messages are forwarded immediately without buffering
- **Routing strategies**: Support for metadata-based, path-based, and header-based routing
- **Service discovery**: Automatic instance registration and health checking
- **Load balancing**: Consistent hashing and round-robin strategies

## Architecture

### Core Flow

```
User → Gateway.Method1(req) [blocks at Junction A]
Gateway → Service Instance (via persistent stream + request ID)
Service Instance → Process request → Send response (via stream)
Gateway → [Junction A unblocks] → Return response to User
```

### Bidirectional Streaming RPC-over-Streams

The system tunnels all gRPC method types through bidirectional streams:

- **RegisterRequest/RegisterResponse**: Bidirectional stream between gateway and service instances
- **Request correlation**: Each RPC call gets a unique request ID for response matching
- **Stream multiplexing**: Multiple concurrent RPCs share the same persistent stream
- **Message forwarding**: No buffering - each message forwarded immediately

## Project Structure

```
grpcrouter/
├── proto/
│   ├── options.proto          # Custom routing annotations
│   └── router.proto           # Router service definition
├── router/
│   ├── correlation.go         # Request correlation system
│   ├── server.go             # Generic router server
│   └── registry.go           # Service instance registry
├── plugin/
│   └── protoc-gen-grpcrouter/ # Code generation plugin
├── examples/
│   └── myservice/             # Example service with routing
├── cmd/
│   └── router/               # Router binary (will be service-specific)
└── Makefile                  # Build automation
```

## Usage

### 1. Define a Service with Routing

```protobuf
syntax = "proto3";

import "proto/options.proto";

service MyService {
  option (grpcrouter.routes_to) = {
    service: "MyService"
    routing_key: "instanceid"
    strategy: METADATA_BASED
    path_prefix: "/api/v1"
  };
  
  rpc Method1(Method1Request) returns (Method1Response);
  rpc Method2(Method2Request) returns (stream Method2Response);
  rpc Method3(stream Method3Request) returns (Method3Response);
  rpc StreamMethod(stream StreamRequest) returns (stream StreamResponse);
}
```

### 2. Generate Router Code

```bash
# Generate protobuf and router code
make proto

# Or manually:
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       --grpcrouter_out=. --grpcrouter_opt=paths=source_relative \
       examples/myservice/service.proto
```

This generates:
- `service.pb.go` - Standard protobuf types
- `service_grpc.pb.go` - Standard gRPC service definitions  
- `service_router.pb.go` - **Generated router implementation**

### 3. Generated Router Implementation

The plugin generates a service-specific router like:

```go
// Generated: myservice_router.pb.go
type MyServiceRouter struct {
    correlator *router.RequestCorrelator
    registry   *router.ServiceRegistry
    config     *router.RouterConfig
}

// Type-safe method implementations
func (r *MyServiceRouter) Method1(ctx context.Context, req *Method1Request) (*Method1Response, error) {
    // Extract routing key and route to appropriate instance
}

func (r *MyServiceRouter) Method2(req *Method2Request, stream MyService_Method2Server) error {
    // Handle server streaming with real-time forwarding
}

// ... other methods
```

### 4. Dual-Mode Service Implementation

Services can run in two modes:

**Direct Mode** (normal gRPC):
```go
func main() {
    server := grpc.NewServer()
    myservice.RegisterMyServiceServer(server, &MyServiceImpl{})
    server.Serve(listener)
}
```

**Router Mode** (via persistent streams):
```go
func main() {
    // Connect to router
    conn, _ := grpc.Dial("router:8080")
    client := pb.NewRouterClient(conn)
    
    // Register with router and handle requests via streams
    stream, _ := client.Register(context.Background())
    handleRegistration(stream, &MyServiceImpl{})
}
```

## Build Commands

```bash
# Install dependencies
make install-deps

# Generate protobuf code
make proto

# Build everything
make build

# Run tests
make test

# Build router binary
make build-router

# Build example service
make build-example

# Clean generated files
make clean
```

## Message Flow Examples

### Unary RPC
```
Client → RouterGateway.Method1(req)
RouterGateway → ServiceInstance: RpcCall{id:123, method, request}
ServiceInstance → RouterGateway: RpcResponse{id:123, response}
RouterGateway → Client: response
```

### Server Streaming
```
Client → RouterGateway.Method2(req) [opens stream]
RouterGateway → ServiceInstance: RpcCall{id:456, method, request}
ServiceInstance → RouterGateway: RpcResponse{id:456, stream_msg1}
RouterGateway → Client: stream_msg1 [immediate forward]
ServiceInstance → RouterGateway: RpcResponse{id:456, stream_msg2}
RouterGateway → Client: stream_msg2 [immediate forward]
ServiceInstance → RouterGateway: RpcResponse{id:456, COMPLETE}
RouterGateway → Client: [close stream]
```

### Bidirectional Streaming
```
Client ↔ RouterGateway.StreamMethod() [bidirectional]
RouterGateway ↔ ServiceInstance: RpcCall/RpcResponse with StreamMessages
Messages flow independently in both directions with sequence numbers
```

## Routing Strategies

- **METADATA_BASED**: Extract routing key from gRPC metadata
- **PATH_BASED**: Extract routing key from HTTP path (for gRPC-Web)
- **HEADER_BASED**: Extract routing key from HTTP headers

## Current Status

✅ Protobuf definitions with custom options
✅ Request correlation system  
✅ Service registry with instance management
✅ Generic router server implementation
🟡 Protoc plugin for code generation (in progress)
⏳ Service-specific router generation
⏳ Dual-mode service wrapper
⏳ Example implementation
⏳ Integration tests

## Next Steps

1. Complete protoc plugin implementation
2. Generate MyService router code
3. Create dual-mode service wrapper
4. Build example binaries
5. End-to-end testing