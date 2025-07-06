#!/bin/bash

echo "=== 4-Phase Router Generation ==="

# Clean previous generation
rm -rf gen/

echo "Step 1: Generate router protos + service bridge + standard Go code from original protos"
buf generate
if [ $? -ne 0 ]; then
    echo "Step 1 failed"
    exit 1
fi

echo "Step 2: Generate Go code for router protos"
# Temporarily copy generated router proto to source location
cp gen/proto/myservice/v1/service_router.proto proto/myservice/v1/

# Generate Go code for the router proto
buf generate --path=proto/myservice/v1/service_router.proto
if [ $? -ne 0 ]; then
    echo "Step 2 failed"
    rm proto/myservice/v1/service_router.proto
    exit 1
fi

# Move generated files to correct location and clean up
mv gen/go/myservice/v1/service_router.pb.go gen/go/myservice/v1/service_router.pb.go.tmp 2>/dev/null || true
mv gen/go/myservice/v1/service_router_grpc.pb.go gen/go/myservice/v1/service_router_grpc.pb.go.tmp 2>/dev/null || true
rm proto/myservice/v1/service_router.proto
mv gen/go/myservice/v1/service_router.pb.go.tmp gen/go/myservice/v1/service_router.pb.go 2>/dev/null || true
mv gen/go/myservice/v1/service_router_grpc.pb.go.tmp gen/go/myservice/v1/service_router_grpc.pb.go 2>/dev/null || true

echo "Step 3: Generate router implementation"
# Copy router proto back to proto directory temporarily for Phase 3
cp gen/proto/myservice/v1/service_router.proto proto/myservice/v1/

# Create a buf configuration that only runs the implementation plugin
cat > buf.gen.phase3.yaml << 'EOF'
version: v1
plugins:
  - name: grpcrouter-impl
    path: ../../cmd/protoc-gen-grpcrouter-impl/protoc-gen-grpcrouter-impl
    out: gen/go
    strategy: directory
EOF

# Generate router implementation from the router proto
buf generate --template=buf.gen.phase3.yaml --path=proto/myservice/v1/service_router.proto
phase3_result=$?

# Clean up
rm -f proto/myservice/v1/service_router.proto buf.gen.phase3.yaml

if [ $phase3_result -eq 0 ]; then
    echo "  ✓ Router implementation generated successfully"
else
    echo "  (Phase 3 plugin needs debugging)"
fi

echo "=== Generation Complete ==="
echo "Generated files:"
echo "  - gen/proto/myservice/v1/service_router.proto (router proto)"
echo "  - gen/go/myservice/v1/service_router.pb.go (typed oneof structs)"  
echo "  - gen/go/myservice/v1/service_router_grpc.pb.go (router server interface)"
echo "  - gen/go/myservice/v1/service_router_impl.pb.go (router implementation)"
echo "  - gen/go/myservice/v1/service_bridge.pb.go (service bridge for easy integration)"