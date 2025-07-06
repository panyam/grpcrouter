# Claude Development Context

## Project: gRPC Router Plugin

### Current State
This is a Go project implementing a gRPC router plugin that enables services to act as routers to other gRPC services using bidirectional streaming over persistent connections.

### Key Architecture Decisions Made

1. **Service-Specific Generated Routers**: Instead of a generic runtime router, we generate service-specific routers with statically-typed method bindings for type safety and performance.

2. **Bidirectional Streaming RPC-over-Streams**: All gRPC method types (unary, server streaming, client streaming, bidirectional streaming) are tunneled through persistent bidirectional streams between gateway and service instances.

3. **Zero Buffering**: Messages are forwarded immediately without buffering to maintain streaming semantics and low latency.

4. **Request Correlation**: Each RPC call gets a unique request ID for response matching across the streams.

### Current Implementation Status

✅ **Completed**:
- Protobuf definitions (`proto/options.proto`, `proto/router.proto`)
- Request correlation system (`router/correlation.go`)
- Service registry (`router/registry.go`) 
- Generic router server (`router/server.go`)
- Project structure and Makefile
- Protoc plugin foundation (`plugin/protoc-gen-grpcrouter/main.go`)

🟡 **In Progress**:
- Protoc plugin for service-specific code generation
- Building the plugin binary

⏳ **Pending**:
- Service-specific router generation
- Dual-mode service wrapper
- Example service implementation
- Integration testing

### Build Commands
```bash
make proto        # Generate protobuf code
make build        # Build everything
make test         # Run tests
make clean        # Clean generated files
```

### Key Files to Remember

- `proto/options.proto` - Custom routing annotations
- `proto/router.proto` - Router service with RpcCall/RpcResponse messages
- `router/correlation.go` - Request ID correlation and pending call management
- `router/server.go` - Generic router server with stream management
- `plugin/protoc-gen-grpcrouter/main.go` - Code generation plugin
- `examples/myservice/service.proto` - Example service with routing annotations
- `Makefile` - Build automation

### Code Generation Strategy

The protoc plugin generates service-specific routers like:
```go
type MyServiceRouter struct {
    correlator *router.RequestCorrelator
    registry   *router.ServiceRegistry
}

func (r *MyServiceRouter) Method1(ctx context.Context, req *Method1Request) (*Method1Response, error) {
    // Type-safe routing implementation
}
```

### Current Task
Completing the protoc plugin build and code generation for the example MyService.

### Dependencies
- `google.golang.org/grpc`
- `google.golang.org/protobuf` 
- `google.golang.org/genproto/googleapis/rpc`

### Testing Strategy
- Unit tests for correlation system
- Integration tests for router functionality  
- End-to-end tests with example service