package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

// Mode determines which generation phase to execute
type Mode string

const (
	ModeProto  Mode = "proto"  // Phase 1: Generate router proto files
	ModeImpl   Mode = "impl"   // Phase 3: Generate router implementation
	ModeBridge Mode = "bridge" // Phase 4: Generate service bridge
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

		// Parse mode from plugin parameter or environment variable
		mode := parseMode(gen.Request.GetParameter())

		switch mode {
		case ModeProto:
			return runProtoGeneration(gen)
		case ModeImpl:
			return runImplGeneration(gen)
		case ModeBridge:
			return runBridgeGeneration(gen)
		default:
			return fmt.Errorf("unknown mode: %s (supported: proto, impl, bridge)", mode)
		}
	})
}

// parseMode determines the generation mode from plugin parameters or environment
func parseMode(parameter string) Mode {
	// Check for parameter from protoc (e.g., mode=proto)
	if parameter != "" {
		for _, opt := range strings.Split(parameter, ",") {
			if strings.HasPrefix(opt, "mode=") {
				return Mode(strings.TrimPrefix(opt, "mode="))
			}
		}
	}

	// Check environment variable as fallback
	if envMode := os.Getenv("GRPCROUTER_MODE"); envMode != "" {
		return Mode(envMode)
	}

	// Default to proto mode for backward compatibility
	return ModeProto
}

// ===================================================================================
// PHASE 1: Proto Generation
// ===================================================================================

func runProtoGeneration(gen *protogen.Plugin) error {
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}

		// Check if this file has services with routing annotations
		if !hasRoutingServices(f) {
			continue
		}

		// Generate router proto file for this service file
		if err := generateRouterProto(gen, f); err != nil {
			return fmt.Errorf("failed to generate router proto for %s: %v", f.Desc.Path(), err)
		}
	}

	return nil
}

// hasRoutingServices checks if the file contains services with routing annotations
func hasRoutingServices(f *protogen.File) bool {
	for _, service := range f.Services {
		if hasRoutingAnnotation(service) {
			return true
		}
	}
	return false
}

// hasRoutingAnnotation checks if a service has routing annotations
func hasRoutingAnnotation(service *protogen.Service) bool {
	opts := service.Desc.Options()
	if opts == nil {
		return false
	}

	if !proto.HasExtension(opts, grpcrouterv1.E_RoutesTo) {
		return false
	}

	routesTo := proto.GetExtension(opts, grpcrouterv1.E_RoutesTo)
	if routesTo == nil {
		return false
	}

	routingOpts, ok := routesTo.(*grpcrouterv1.RoutingOptions)
	if !ok || routingOpts == nil {
		return false
	}

	return routingOpts.Service != ""
}

// generateRouterProto generates a service-specific router proto file
func generateRouterProto(gen *protogen.Plugin, f *protogen.File) error {
	// Create output filename
	baseFileName := strings.TrimSuffix(string(f.Desc.Path()), ".proto")
	outputPath := baseFileName + "_router.proto"

	// Create the proto file content
	content, err := buildRouterProtoContent(f)
	if err != nil {
		return fmt.Errorf("failed to build router proto content: %v", err)
	}

	// Write the generated proto file
	gen.NewGeneratedFile(outputPath, "").P(content)

	log.Printf("Generated router proto: %s", outputPath)
	return nil
}

// buildRouterProtoContent builds the content of the router proto file
func buildRouterProtoContent(f *protogen.File) (string, error) {
	var sb strings.Builder

	// Proto file header
	sb.WriteString("syntax = \"proto3\";\n\n")
	
	// Package declaration
	if f.Desc.Package() != "" {
		sb.WriteString(fmt.Sprintf("package %s;\n\n", f.Desc.Package()))
	}

	// Go package option  
	if f.GoImportPath != "" {
		goImport := string(f.GoImportPath)
		// Remove surrounding quotes if they exist
		goImport = strings.Trim(goImport, "\"")
		sb.WriteString(fmt.Sprintf("option go_package = \"%s\";\n\n", goImport))
	}

	// Imports
	sb.WriteString("import \"grpcrouter/v1/router.proto\";\n")
	sb.WriteString(fmt.Sprintf("import \"%s\";\n\n", f.Desc.Path()))

	// Generate router content for each service with routing annotations
	for _, service := range f.Services {
		if !hasRoutingAnnotation(service) {
			continue
		}

		if err := generateServiceRouter(f, service, &sb); err != nil {
			return "", fmt.Errorf("failed to generate router for service %s: %v", service.GoName, err)
		}
	}

	return sb.String(), nil
}

// generateServiceRouter generates the router service and related messages for a specific service
func generateServiceRouter(f *protogen.File, service *protogen.Service, sb *strings.Builder) error {
	serviceName := service.GoName
	
	// Generate the router service
	sb.WriteString(fmt.Sprintf("// %sRouter provides routing for %s service\n", serviceName, serviceName))
	sb.WriteString(fmt.Sprintf("service %sRouter {\n", serviceName))

	// Add all original service methods as proxy methods
	for _, method := range service.Methods {
		sb.WriteString(fmt.Sprintf("  // Proxy for %s.%s\n", serviceName, method.GoName))
		
		// Build method signature
		inputType := getTypeName(method.Input)
		outputType := getTypeName(method.Output)
		
		if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
			// Bidirectional streaming
			sb.WriteString(fmt.Sprintf("  rpc %s(stream %s) returns (stream %s);\n", method.GoName, inputType, outputType))
		} else if method.Desc.IsStreamingClient() {
			// Client streaming
			sb.WriteString(fmt.Sprintf("  rpc %s(stream %s) returns (%s);\n", method.GoName, inputType, outputType))
		} else if method.Desc.IsStreamingServer() {
			// Server streaming
			sb.WriteString(fmt.Sprintf("  rpc %s(%s) returns (stream %s);\n", method.GoName, inputType, outputType))
		} else {
			// Unary
			sb.WriteString(fmt.Sprintf("  rpc %s(%s) returns (%s);\n", method.GoName, inputType, outputType))
		}
	}

	// Add the registration method
	sb.WriteString("\n  // Service instance registration and RPC routing\n")
	sb.WriteString(fmt.Sprintf("  rpc Register(stream %sRegisterRequest) returns (stream %sRegisterResponse);\n", serviceName, serviceName))
	sb.WriteString("}\n\n")

	// Generate service-specific request/response messages
	if err := generateServiceMessages(service, sb); err != nil {
		return fmt.Errorf("failed to generate service messages: %v", err)
	}

	return nil
}

// generateServiceMessages generates the service-specific oneof messages
func generateServiceMessages(service *protogen.Service, sb *strings.Builder) error {
	serviceName := service.GoName

	// Generate RegisterRequest message
	sb.WriteString(fmt.Sprintf("// %sRegisterRequest contains registration and RPC response messages\n", serviceName))
	sb.WriteString(fmt.Sprintf("message %sRegisterRequest {\n", serviceName))
	sb.WriteString("  oneof request {\n")
	sb.WriteString("    // Standard registration messages\n")
	sb.WriteString("    grpcrouter.v1.InstanceInfo instance_info = 1;\n")
	sb.WriteString("    grpcrouter.v1.Heartbeat heartbeat = 2;\n")
	sb.WriteString("    grpcrouter.v1.UnregisterRequest unregister = 3;\n")
	sb.WriteString("    // Service-specific RPC response\n")
	sb.WriteString(fmt.Sprintf("    %sRpcResponse rpc_response = 4;\n", serviceName))
	sb.WriteString("  }\n")
	sb.WriteString("}\n\n")

	// Generate RegisterResponse message
	sb.WriteString(fmt.Sprintf("// %sRegisterResponse contains registration acks and RPC calls\n", serviceName))
	sb.WriteString(fmt.Sprintf("message %sRegisterResponse {\n", serviceName))
	sb.WriteString("  oneof response {\n")
	sb.WriteString("    // Standard registration messages\n")
	sb.WriteString("    grpcrouter.v1.RegistrationAck ack = 1;\n")
	sb.WriteString("    grpcrouter.v1.ErrorResponse error = 2;\n")
	sb.WriteString("    // Service-specific RPC call\n")
	sb.WriteString(fmt.Sprintf("    %sRpcCall rpc_call = 3;\n", serviceName))
	sb.WriteString("  }\n")
	sb.WriteString("}\n\n")

	// Generate RPC call message with oneof for all request types
	sb.WriteString(fmt.Sprintf("// %sRpcCall contains typed RPC requests for %s methods\n", serviceName, serviceName))
	sb.WriteString(fmt.Sprintf("message %sRpcCall {\n", serviceName))
	sb.WriteString("  string request_id = 1;\n")
	sb.WriteString("  string method = 2;\n")
	sb.WriteString("  grpcrouter.v1.RpcMethodType method_type = 3;\n")
	sb.WriteString("  map<string, string> metadata = 4;\n")
	sb.WriteString("  \n")
	sb.WriteString("  oneof request {\n")

	// Add each method's request type
	fieldNum := 10
	for _, method := range service.Methods {
		requestType := getTypeName(method.Input)
		fieldName := toSnakeCase(method.GoName)
		sb.WriteString(fmt.Sprintf("    %s %s = %d;\n", requestType, fieldName, fieldNum))
		fieldNum++
	}

	sb.WriteString("  }\n")
	sb.WriteString("}\n\n")

	// Generate RPC response message with oneof for all response types
	sb.WriteString(fmt.Sprintf("// %sRpcResponse contains typed RPC responses for %s methods\n", serviceName, serviceName))
	sb.WriteString(fmt.Sprintf("message %sRpcResponse {\n", serviceName))
	sb.WriteString("  string request_id = 1;\n")
	sb.WriteString("  map<string, string> metadata = 2;\n")
	sb.WriteString("  grpcrouter.v1.RpcStatus status = 3;\n")
	sb.WriteString("  \n")
	sb.WriteString("  oneof response {\n")

	// Add each method's response type
	fieldNum = 10
	for _, method := range service.Methods {
		responseType := getTypeName(method.Output)
		fieldName := toSnakeCase(method.GoName)
		sb.WriteString(fmt.Sprintf("    %s %s = %d;\n", responseType, fieldName, fieldNum))
		fieldNum++
	}

	sb.WriteString("  }\n")
	sb.WriteString("}\n\n")

	return nil
}

// getTypeName extracts the type name from a protogen message
func getTypeName(msg *protogen.Message) string {
	// Get the full protobuf name and extract just the message name
	fullName := string(msg.Desc.FullName())
	parts := strings.Split(fullName, ".")
	return parts[len(parts)-1]
}

// toSnakeCase converts PascalCase to snake_case for proto field names
func toSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && 'A' <= r && r <= 'Z' {
			result = append(result, '_')
		}
		if 'A' <= r && r <= 'Z' {
			result = append(result, r-'A'+'a')
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// ===================================================================================
// PHASE 3: Implementation Generation
// ===================================================================================

func runImplGeneration(gen *protogen.Plugin) error {
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}

		// Only process generated router proto files
		if !isGeneratedRouterProto(f) {
			continue
		}

		// Generate router implementation for this router proto
		if err := generateRouterImplementation(gen, f); err != nil {
			return fmt.Errorf("failed to generate router implementation for %s: %v", f.Desc.Path(), err)
		}
	}

	return nil
}

// isGeneratedRouterProto checks if this is a generated router proto file
func isGeneratedRouterProto(f *protogen.File) bool {
	// Check if filename contains "_router" and has router services
	if !strings.Contains(string(f.Desc.Path()), "_router") {
		return false
	}

	// Check if it has services ending with "Router"
	for _, service := range f.Services {
		if strings.HasSuffix(service.GoName, "Router") {
			return true
		}
	}
	
	return false
}

// generateRouterImplementation generates the router implementation
func generateRouterImplementation(gen *protogen.Plugin, f *protogen.File) error {
	// Find the router service
	var routerService *protogen.Service
	for _, service := range f.Services {
		if strings.HasSuffix(service.GoName, "Router") {
			routerService = service
			break
		}
	}

	if routerService == nil {
		return fmt.Errorf("no router service found in file %s", f.Desc.Path())
	}

	// Create output filename
	baseFileName := strings.TrimSuffix(string(f.Desc.Path()), ".proto")
	outputPath := baseFileName + "_impl.pb.go"

	// Generate the implementation file
	genFile := gen.NewGeneratedFile(outputPath, f.GoImportPath)
	
	if err := generateImplementationContent(genFile, f, routerService); err != nil {
		return fmt.Errorf("failed to generate implementation content: %v", err)
	}

	log.Printf("Generated router implementation: %s", outputPath)
	return nil
}

// generateImplementationContent generates the router implementation content
func generateImplementationContent(g *protogen.GeneratedFile, f *protogen.File, routerService *protogen.Service) error {
	serviceName := strings.TrimSuffix(routerService.GoName, "Router")
	implName := routerService.GoName + "Impl"

	// Package and imports
	g.P("// Code generated by protoc-gen-grpcrouter. DO NOT EDIT.")
	g.P("// source: ", f.Desc.Path())
	g.P()
	g.P("package ", f.GoPackageName)
	g.P()
	g.P("import (")
	g.P(`"context"`)
	g.P(`"log"`)
	g.P(`"strconv"`)
	g.P(`"sync"`) 
	g.P(`"time"`)
	g.P()
	g.P(`"google.golang.org/grpc/codes"`)
	g.P(`"google.golang.org/grpc/metadata"`)
	g.P(`"google.golang.org/grpc/status"`)
	g.P()
	g.P(`"github.com/panyam/grpcrouter/router"`)
	g.P(`pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"`)
	g.P(")")
	g.P()

	// Generate the router implementation struct
	g.P("// ", implName, " implements ", routerService.GoName, "Server with routing capabilities")
	g.P("type ", implName, " struct {")
	g.P("Unimplemented", routerService.GoName, "Server")
	g.P()
	g.P("// Core routing components")
	g.P("correlator *router.RequestCorrelator")
	g.P("registry   *router.ServiceRegistry")
	g.P("config     *router.RouterConfig")
	g.P()
	g.P("// Service instance management")
	g.P("instances    map[string]*router.ServiceInstance")
	g.P("instancesMux sync.RWMutex")
	g.P("}")
	g.P()

	// Generate constructor
	g.P("// New", implName, " creates a new ", serviceName, " router")
	g.P("func New", implName, "(config *router.RouterConfig) *", implName, " {")
	g.P("if config == nil {")
	g.P("config = &router.RouterConfig{")
	g.P("DefaultTimeout:      30 * time.Second,")
	g.P("MaxConcurrentRPCs:   1000,")
	g.P("HealthCheckInterval: 30 * time.Second,")
	g.P("}")
	g.P("}")
	g.P("return &", implName, "{")
	g.P("correlator: router.NewRequestCorrelator(),")
	g.P("registry:   router.NewServiceRegistry(),")
	g.P("config:     config,")
	g.P("instances:  make(map[string]*router.ServiceInstance),")
	g.P("}")
	g.P("}")
	g.P()

	// Generate proxy methods for each service method
	for _, method := range routerService.Methods {
		if method.GoName == "Register" {
			// Skip Register - we'll handle it separately
			continue
		}
		
		if err := generateProxyMethod(g, serviceName, method); err != nil {
			return fmt.Errorf("failed to generate proxy method %s: %v", method.GoName, err)
		}
	}

	// Generate Register method
	if err := generateRegisterMethod(g, serviceName, implName); err != nil {
		return fmt.Errorf("failed to generate Register method: %v", err)
	}

	// Generate helper methods
	if err := generateHelperMethods(g, serviceName, implName); err != nil {
		return fmt.Errorf("failed to generate helper methods: %v", err)
	}

	return nil
}

// generateProxyMethod generates a proxy method that routes calls to service instances
func generateProxyMethod(g *protogen.GeneratedFile, serviceName string, method *protogen.Method) error {
	methodName := method.GoName
	
	if method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer() {
		// Bidirectional streaming
		g.P("// ", methodName, " implements the ", methodName, " method with routing")
		g.P("func (r *", serviceName, "RouterImpl) ", methodName, "(stream ", serviceName, "Router_", methodName, "Server) error {")
		g.P("// TODO: Implement bidirectional streaming routing")
		g.P(`return status.Errorf(codes.Unimplemented, "bidirectional streaming not yet implemented")`)
		g.P("}")
		g.P()
	} else if method.Desc.IsStreamingClient() {
		// Client streaming
		g.P("// ", methodName, " implements the ", methodName, " method with routing")
		g.P("func (r *", serviceName, "RouterImpl) ", methodName, "(stream ", serviceName, "Router_", methodName, "Server) error {")
		g.P("// TODO: Implement client streaming routing")
		g.P(`return status.Errorf(codes.Unimplemented, "client streaming not yet implemented")`)
		g.P("}")
		g.P()
	} else if method.Desc.IsStreamingServer() {
		// Server streaming  
		g.P("// ", methodName, " implements the ", methodName, " method with routing")
		g.P("func (r *", serviceName, "RouterImpl) ", methodName, "(req *", method.Input.GoIdent.GoName, ", stream ", serviceName, "Router_", methodName, "Server) error {")
		g.P("// TODO: Implement server streaming routing")
		g.P(`return status.Errorf(codes.Unimplemented, "server streaming not yet implemented")`)
		g.P("}")
		g.P()
	} else {
		// Unary RPC
		g.P("// ", methodName, " implements the ", methodName, " method with routing")
		g.P("func (r *", serviceName, "RouterImpl) ", methodName, "(ctx context.Context, req *", method.Input.GoIdent.GoName, ") (*", method.Output.GoIdent.GoName, ", error) {")
		g.P("// Extract routing key from metadata or request")
		g.P("routingKey := r.extractRoutingKey(ctx, req)")
		g.P()
		g.P("// Find appropriate instance")
		g.P(`instance, err := r.registry.SelectInstance("`, serviceName, `", routingKey)`)
		g.P("if err != nil {")
		g.P(`return nil, status.Errorf(codes.Unavailable, "no available instance for service `, serviceName, `: %v", err)`)
		g.P("}")
		g.P()
		g.P("// Create typed RPC call")
		g.P("call := &", serviceName, "RpcCall{")
		g.P(`RequestId:  "req-" + strconv.FormatInt(time.Now().UnixNano(), 10),`)
		g.P(`Method:     "/`, serviceName, "/", methodName, `",`)
		g.P("MethodType: pb.RpcMethodType_UNARY,")
		g.P("Metadata:   make(map[string]string),")
		g.P("Request:    &", serviceName, "RpcCall_", methodName, "{", methodName, ": req},")
		g.P("}")
		g.P()
		g.P("// Send typed call to instance and wait for typed response")
		g.P("response, err := r.sendTypedCall(instance, call)")
		g.P("if err != nil {")
		g.P("return nil, err")
		g.P("}")
		g.P()
		g.P("// Extract typed response")
		g.P("if resp := response.Get", methodName, "(); resp != nil {")
		g.P("return resp, nil")
		g.P("}")
		g.P()
		g.P(`return nil, status.Errorf(codes.Internal, "invalid response type for `, methodName, `")`)
		g.P("}")
		g.P()
	}

	return nil
}

// generateRegisterMethod generates the Register method implementation
func generateRegisterMethod(g *protogen.GeneratedFile, serviceName, implName string) error {
	g.P("// Register handles service instance registration with typed messages")
	g.P("func (r *", implName, ") Register(stream ", serviceName, "Router_RegisterServer) error {")
	g.P("// Wait for initial registration message")
	g.P("req, err := stream.Recv()")
	g.P("if err != nil {")
	g.P(`return status.Errorf(codes.Internal, "failed to receive registration: %v", err)`)
	g.P("}")
	g.P()
	g.P("// Extract instance info")
	g.P("instanceInfo := req.GetInstanceInfo()")
	g.P("if instanceInfo == nil {")
	g.P(`return status.Errorf(codes.InvalidArgument, "expected instance info in first message")`)
	g.P("}")
	g.P()
	g.P("// Validate service name")
	g.P(`if instanceInfo.ServiceName != "`, serviceName, `" {`)
	g.P(`return status.Errorf(codes.InvalidArgument, "service name mismatch: expected `, serviceName, `, got %s", instanceInfo.ServiceName)`)
	g.P("}")
	g.P()
	g.P("// Create service instance")
	g.P("instance := &router.ServiceInstance{")
	g.P("InstanceID:    instanceInfo.InstanceId,")
	g.P(`ServiceName:   "`, serviceName, `",`)
	g.P("Endpoint:      instanceInfo.Endpoint,")
	g.P("Metadata:      instanceInfo.Metadata,")
	g.P("HealthStatus:  instanceInfo.HealthStatus,")
	g.P("RegisteredAt:  time.Now(),")
	g.P("LastHeartbeat: time.Now(),")
	g.P("RequestQueue:  make(chan *pb.RpcCall, 100),")
	g.P("ResponseQueue: make(chan *pb.RpcResponse, 100),")
	g.P("Connected:     true,")
	g.P("}")
	g.P()
	g.P("// Register instance")
	g.P("r.instancesMux.Lock()")
	g.P("r.instances[instanceInfo.InstanceId] = instance")
	g.P("r.instancesMux.Unlock()")
	g.P()
	g.P("if err := r.registry.RegisterInstance(instance); err != nil {")
	g.P("r.instancesMux.Lock()")
	g.P("delete(r.instances, instanceInfo.InstanceId)")
	g.P("r.instancesMux.Unlock()")
	g.P(`return status.Errorf(codes.AlreadyExists, "failed to register instance: %v", err)`)
	g.P("}")
	g.P()
	g.P("// Send registration acknowledgment")
	g.P("ack := &", serviceName, "RegisterResponse{")
	g.P("Response: &", serviceName, "RegisterResponse_Ack{")
	g.P("Ack: &pb.RegistrationAck{")
	g.P("InstanceId: instanceInfo.InstanceId,")
	g.P("Success:    true,")
	g.P(`Message:    "Registration successful",`)
	g.P("},")
	g.P("},")
	g.P("}")
	g.P()
	g.P("if err := stream.Send(ack); err != nil {")
	g.P("r.unregisterInstance(instanceInfo.InstanceId)")
	g.P(`return status.Errorf(codes.Internal, "failed to send acknowledgment: %v", err)`)
	g.P("}")
	g.P()
	g.P(`log.Printf("Service instance registered: %s (`, serviceName, `)", instanceInfo.InstanceId)`)
	g.P()
	g.P("// Start instance handling goroutines")
	g.P("go r.handleInstanceRequests(instance)")
	g.P("go r.handleInstanceResponses(instance)")
	g.P()
	g.P("// Handle incoming messages from the instance")
	g.P("return r.handleInstanceMessages(instance)")
	g.P("}")
	g.P()

	return nil
}

// generateHelperMethods generates helper methods for the router implementation
func generateHelperMethods(g *protogen.GeneratedFile, serviceName, implName string) error {
	// extractRoutingKey method
	g.P("// extractRoutingKey extracts the routing key from context or request")
	g.P("func (r *", implName, ") extractRoutingKey(ctx context.Context, req interface{}) string {")
	g.P("// Strategy 1: Extract from gRPC metadata (primary)")
	g.P(`if md, ok := metadata.FromIncomingContext(ctx); ok {`)
	g.P("// Check common routing key metadata fields")
	g.P(`if values := md.Get("instanceid"); len(values) > 0 {`)
	g.P("return values[0]")
	g.P("}")
	g.P(`if values := md.Get("routing-key"); len(values) > 0 {`)
	g.P("return values[0]")
	g.P("}")
	g.P(`if values := md.Get("service-instance"); len(values) > 0 {`)
	g.P("return values[0]")
	g.P("}")
	g.P("}")
	g.P()
	g.P("// Strategy 2: Default routing (round-robin)")
	g.P("// If no routing key specified, use empty string for default selection")
	g.P(`return ""`)
	g.P("}")
	g.P()

	// sendTypedCall method
	g.P("// sendTypedCall sends a typed RPC call to an instance and waits for typed response")
	g.P("func (r *", implName, ") sendTypedCall(instance *router.ServiceInstance, call *", serviceName, "RpcCall) (*", serviceName, "RpcResponse, error) {")
	g.P("// For now, return placeholder error until full implementation")
	g.P("// The correct implementation requires:")
	g.P("// 1. Fixing stream type compatibility between typed and generic streams")
	g.P("// 2. Using CreatePendingCall from correlator with proper method signature")
	g.P("// 3. Implementing typed response correlation")
	g.P()
	g.P("_ = instance // Avoid unused variable warning")
	g.P("_ = call     // Avoid unused variable warning")
	g.P()
	g.P(`return nil, status.Errorf(codes.Unimplemented, "typed call routing requires stream type compatibility fixes")`)
	g.P("}")
	g.P()

	// Instance management methods
	g.P("// unregisterInstance removes an instance from the router")
	g.P("func (r *", implName, ") unregisterInstance(instanceID string) {")
	g.P("r.instancesMux.Lock()")
	g.P("delete(r.instances, instanceID)")
	g.P("r.instancesMux.Unlock()")
	g.P("r.registry.UnregisterInstance(instanceID)")
	g.P("}")
	g.P()

	g.P("// handleInstanceMessages processes incoming messages from a service instance")
	g.P("func (r *", implName, ") handleInstanceMessages(instance *router.ServiceInstance) error {")
	g.P("// This method would handle ongoing communication with service instances")
	g.P("// For now, the Register method handles the initial registration stream")
	g.P("// This method is called but immediately returns until stream types are aligned")
	g.P()
	g.P("_ = instance // Avoid unused variable warning")
	g.P()
	g.P("// TODO: Implement once stream type compatibility is resolved")
	g.P("// This would handle:")
	g.P("// 1. Incoming RPC responses from service instances")
	g.P("// 2. Heartbeat messages and health status updates")
	g.P("// 3. Instance disconnection and reconnection")
	g.P()
	g.P(`return status.Errorf(codes.Unimplemented, "instance message handling needs stream type fixes")`)
	g.P("}")
	g.P()

	g.P("// handleInstanceRequests sends RPC requests to the service instance") 
	g.P("func (r *", implName, ") handleInstanceRequests(instance *router.ServiceInstance) {")
	g.P("// TODO: Implement instance request handling")
	g.P("}")
	g.P()

	g.P("// handleInstanceResponses processes RPC responses from the service instance")
	g.P("func (r *", implName, ") handleInstanceResponses(instance *router.ServiceInstance) {")
	g.P("// TODO: Implement instance response handling")
	g.P("}")
	g.P()

	return nil
}

// ===================================================================================
// PHASE 4: Bridge Generation
// ===================================================================================

func runBridgeGeneration(gen *protogen.Plugin) error {
	for _, f := range gen.Files {
		if !f.Generate {
			continue
		}

		// Only process original service proto files (not generated router protos)
		if isGeneratedRouterProto(f) {
			continue
		}

		// Check if file has services that could benefit from bridge generation
		for _, service := range f.Services {
			if err := generateServiceBridge(gen, f, service); err != nil {
				return fmt.Errorf("failed to generate bridge for service %s: %v", service.GoName, err)
			}
		}
	}

	return nil
}

// generateServiceBridge generates the service bridge for a given service
func generateServiceBridge(gen *protogen.Plugin, f *protogen.File, service *protogen.Service) error {
	serviceName := service.GoName
	bridgeName := serviceName + "Bridge"
	
	// Create output filename
	baseFileName := strings.TrimSuffix(string(f.Desc.Path()), ".proto")
	outputPath := baseFileName + "_bridge.pb.go"

	// Generate the bridge file
	genFile := gen.NewGeneratedFile(outputPath, f.GoImportPath)
	
	if err := generateBridgeContent(genFile, f, service, serviceName, bridgeName); err != nil {
		return fmt.Errorf("failed to generate bridge content: %v", err)
	}

	log.Printf("Generated service bridge: %s", outputPath)
	return nil
}

// generateBridgeContent generates the service bridge implementation
func generateBridgeContent(g *protogen.GeneratedFile, f *protogen.File, service *protogen.Service, serviceName, bridgeName string) error {
	// Package and imports
	g.P("// Code generated by protoc-gen-grpcrouter. DO NOT EDIT.")
	g.P("// source: ", f.Desc.Path())
	g.P()
	g.P("package ", f.GoPackageName)
	g.P()
	g.P("import (")
	g.P(`"context"`)
	g.P(`"fmt"`)
	g.P(`"log"`)
	g.P(`"sync"`)
	g.P(`"time"`)
	g.P()
	g.P(`"google.golang.org/grpc"`)
	g.P(`"google.golang.org/grpc/credentials/insecure"`)
	g.P(`"google.golang.org/protobuf/types/known/timestamppb"`)
	g.P()
	g.P(`pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"`)
	g.P(")")
	g.P()

	// Generate bridge options struct
	if err := generateBridgeOptions(g, serviceName); err != nil {
		return err
	}

	// Generate main bridge struct
	if err := generateBridgeStruct(g, serviceName, bridgeName); err != nil {
		return err
	}

	// Generate constructor
	if err := generateBridgeConstructor(g, serviceName, bridgeName); err != nil {
		return err
	}

	// Generate lifecycle methods
	if err := generateBridgeLifecycle(g, serviceName, bridgeName); err != nil {
		return err
	}

	// Generate connection management
	if err := generateConnectionManagement(g, serviceName, bridgeName); err != nil {
		return err
	}

	// Generate RPC dispatch logic
	if err := generateRPCDispatch(g, service, serviceName, bridgeName); err != nil {
		return err
	}

	return nil
}

// generateBridgeOptions generates the bridge configuration options
func generateBridgeOptions(g *protogen.GeneratedFile, serviceName string) error {
	g.P("// ", serviceName, "BridgeOptions configures the service bridge")
	g.P("type ", serviceName, "BridgeOptions struct {")
	g.P("// Instance identification")
	g.P("InstanceID string")
	g.P("Endpoint   string")
	g.P()
	g.P("// Connection settings")
	g.P("RouterAddr         string")
	g.P("ReconnectInterval  time.Duration")
	g.P("HeartbeatInterval  time.Duration")
	g.P()
	g.P("// Service metadata")
	g.P("Metadata map[string]string")
	g.P()
	g.P("// Optional custom context")
	g.P("Context context.Context")
	g.P("}")
	g.P()

	// Generate default options function
	g.P("// Default", serviceName, "BridgeOptions returns sensible defaults")
	g.P("func Default", serviceName, "BridgeOptions() *", serviceName, "BridgeOptions {")
	g.P("return &", serviceName, "BridgeOptions{")
	g.P(`InstanceID:        fmt.Sprintf("`, serviceName, `-bridge-%d", time.Now().Unix()%10000),`)
	g.P(`RouterAddr:        "localhost:9080",`)
	g.P("ReconnectInterval: 5 * time.Second,")
	g.P("HeartbeatInterval: 30 * time.Second,")
	g.P("Metadata:          make(map[string]string),")
	g.P("Context:           context.Background(),")
	g.P("}")
	g.P("}")
	g.P()

	return nil
}

// generateBridgeStruct generates the main bridge struct
func generateBridgeStruct(g *protogen.GeneratedFile, serviceName, bridgeName string) error {
	g.P("// ", bridgeName, " wraps a ", serviceName, "Server and connects it to a ", serviceName, "Router")
	g.P("type ", bridgeName, " struct {")
	g.P("// Service implementation")
	g.P("service ", serviceName, "Server")
	g.P()
	g.P("// Configuration")
	g.P("options *", serviceName, "BridgeOptions")
	g.P()
	g.P("// Connection state")
	g.P("conn     *grpc.ClientConn")
	g.P("client   ", serviceName, "RouterClient")
	g.P("stream   ", serviceName, "Router_RegisterClient")
	g.P()
	g.P("// Lifecycle management")
	g.P("ctx      context.Context")
	g.P("cancel   context.CancelFunc")
	g.P("started  bool")
	g.P("stopped  bool")
	g.P("done     chan struct{}")
	g.P("mu       sync.RWMutex")
	g.P("}")
	g.P()

	return nil
}

// generateBridgeConstructor generates the bridge constructor
func generateBridgeConstructor(g *protogen.GeneratedFile, serviceName, bridgeName string) error {
	g.P("// New", bridgeName, " creates a new service bridge")
	g.P("func New", bridgeName, "(service ", serviceName, "Server, options *", serviceName, "BridgeOptions) *", bridgeName, " {")
	g.P("if options == nil {")
	g.P("options = Default", serviceName, "BridgeOptions()")
	g.P("}")
	g.P()
	g.P("// Auto-generate endpoint if not provided")
	g.P("if options.Endpoint == \"\" {")
	g.P(`options.Endpoint = fmt.Sprintf("bridge-%s", options.InstanceID)`)
	g.P("}")
	g.P()
	g.P("// Set up service metadata")
	g.P("if options.Metadata == nil {")
	g.P("options.Metadata = make(map[string]string)")
	g.P("}")
	g.P(`options.Metadata["bridge_type"] = "auto-generated"`)
	g.P(`options.Metadata["service_type"] = "`, serviceName, `"`)
	g.P(`options.Metadata["routing_key"] = options.InstanceID`)
	g.P()
	g.P("ctx := options.Context")
	g.P("if ctx == nil {")
	g.P("ctx = context.Background()")
	g.P("}")
	g.P()
	g.P("return &", bridgeName, "{")
	g.P("service: service,")
	g.P("options: options,")
	g.P("done:    make(chan struct{}),")
	g.P("}")
	g.P("}")
	g.P()

	return nil
}

// generateBridgeLifecycle generates Start, Stop, and Wait methods
func generateBridgeLifecycle(g *protogen.GeneratedFile, serviceName, bridgeName string) error {
	// Start method
	g.P("// Start connects the bridge to the router and begins processing")
	g.P("func (b *", bridgeName, ") Start() error {")
	g.P("b.mu.Lock()")
	g.P("defer b.mu.Unlock()")
	g.P()
	g.P("if b.started {")
	g.P(`return fmt.Errorf("bridge already started")`)
	g.P("}")
	g.P()
	g.P("if b.stopped {")
	g.P(`return fmt.Errorf("bridge has been stopped and cannot be restarted")`)
	g.P("}")
	g.P()
	g.P("// Set up context")
	g.P("b.ctx, b.cancel = context.WithCancel(b.options.Context)")
	g.P()
	g.P("// Connect and start")
	g.P("if err := b.connect(); err != nil {")
	g.P(`return fmt.Errorf("failed to connect: %v", err)`)
	g.P("}")
	g.P()
	g.P("b.started = true")
	g.P(`log.Printf("[%s] `, serviceName, `Bridge started", b.options.InstanceID)`)
	g.P("return nil")
	g.P("}")
	g.P()

	// Stop method
	g.P("// Stop gracefully shuts down the bridge")
	g.P("func (b *", bridgeName, ") Stop() {")
	g.P("b.mu.Lock()")
	g.P("if !b.started || b.stopped {")
	g.P("b.mu.Unlock()")
	g.P("return")
	g.P("}")
	g.P("b.stopped = true")
	g.P("b.mu.Unlock()")
	g.P()
	g.P("// Cancel context to stop all goroutines")
	g.P("if b.cancel != nil {")
	g.P("b.cancel()")
	g.P("}")
	g.P()
	g.P("// Close connection")
	g.P("if b.conn != nil {")
	g.P("b.conn.Close()")
	g.P("}")
	g.P()
	g.P("// Signal completion")
	g.P("close(b.done)")
	g.P(`log.Printf("[%s] `, serviceName, `Bridge stopped", b.options.InstanceID)`)
	g.P("}")
	g.P()

	// Wait method
	g.P("// Wait blocks until the bridge is stopped")
	g.P("func (b *", bridgeName, ") Wait() {")
	g.P("<-b.done")
	g.P("}")
	g.P()

	return nil
}

// generateConnectionManagement generates connection and registration logic
func generateConnectionManagement(g *protogen.GeneratedFile, serviceName, bridgeName string) error {
	// Connect method
	g.P("// connect establishes connection to router and handles registration")
	g.P("func (b *", bridgeName, ") connect() error {")
	g.P("// Connect to router")
	g.P("conn, err := grpc.Dial(b.options.RouterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))")
	g.P("if err != nil {")
	g.P(`return fmt.Errorf("failed to dial router at %s: %v", b.options.RouterAddr, err)`)
	g.P("}")
	g.P()
	g.P("b.conn = conn")
	g.P("b.client = New", serviceName, "RouterClient(conn)")
	g.P()
	g.P("// Create registration stream")
	g.P("stream, err := b.client.Register(b.ctx)")
	g.P("if err != nil {")
	g.P(`return fmt.Errorf("failed to create registration stream: %v", err)`)
	g.P("}")
	g.P("b.stream = stream")
	g.P()
	g.P("// Send initial registration")
	g.P("if err := b.sendRegistration(); err != nil {")
	g.P(`return fmt.Errorf("failed to send registration: %v", err)`)
	g.P("}")
	g.P()
	g.P("// Start background goroutines")
	g.P("go b.handleHeartbeats()")
	g.P("go b.handleRegistrationResponses()")
	g.P()
	g.P("return nil")
	g.P("}")
	g.P()

	// Send registration method
	g.P("// sendRegistration sends the initial registration message")
	g.P("func (b *", bridgeName, ") sendRegistration() error {")
	g.P("req := &", serviceName, "RegisterRequest{")
	g.P("Request: &", serviceName, "RegisterRequest_InstanceInfo{")
	g.P("InstanceInfo: &pb.InstanceInfo{")
	g.P("InstanceId:   b.options.InstanceID,")
	g.P(`ServiceName:  "`, serviceName, `",`)
	g.P("Endpoint:     b.options.Endpoint,")
	g.P("Metadata:     b.options.Metadata,")
	g.P("HealthStatus: pb.HealthStatus_HEALTHY,")
	g.P("RegisteredAt: timestamppb.Now(),")
	g.P("},")
	g.P("},")
	g.P("}")
	g.P()
	g.P("return b.stream.Send(req)")
	g.P("}")
	g.P()

	// Heartbeat handling
	g.P("// handleHeartbeats sends periodic heartbeat messages")
	g.P("func (b *", bridgeName, ") handleHeartbeats() {")
	g.P("ticker := time.NewTicker(b.options.HeartbeatInterval)")
	g.P("defer ticker.Stop()")
	g.P()
	g.P("for {")
	g.P("select {")
	g.P("case <-ticker.C:")
	g.P("heartbeat := &", serviceName, "RegisterRequest{")
	g.P("Request: &", serviceName, "RegisterRequest_Heartbeat{")
	g.P("Heartbeat: &pb.Heartbeat{")
	g.P("InstanceId:   b.options.InstanceID,")
	g.P("Timestamp:    timestamppb.Now(),")
	g.P("HealthStatus: pb.HealthStatus_HEALTHY,")
	g.P("},")
	g.P("},")
	g.P("}")
	g.P()
	g.P("if err := b.stream.Send(heartbeat); err != nil {")
	g.P(`log.Printf("[%s] Failed to send heartbeat: %v", b.options.InstanceID, err)`)
	g.P("return")
	g.P("}")
	g.P()
	g.P("case <-b.ctx.Done():")
	g.P("return")
	g.P("}")
	g.P("}")
	g.P("}")
	g.P()

	// Registration response handling
	g.P("// handleRegistrationResponses processes messages from the router")
	g.P("func (b *", bridgeName, ") handleRegistrationResponses() {")
	g.P("for {")
	g.P("select {")
	g.P("case <-b.ctx.Done():")
	g.P("return")
	g.P("default:")
	g.P("}")
	g.P()
	g.P("resp, err := b.stream.Recv()")
	g.P("if err != nil {")
	g.P(`log.Printf("[%s] Registration stream error: %v", b.options.InstanceID, err)`)
	g.P("return")
	g.P("}")
	g.P()
	g.P("switch msg := resp.Response.(type) {")
	g.P("case *", serviceName, "RegisterResponse_Ack:")
	g.P("if msg.Ack.Success {")
	g.P(`log.Printf("[%s] Registration acknowledged: %s", b.options.InstanceID, msg.Ack.Message)`)
	g.P("} else {")
	g.P(`log.Printf("[%s] Registration failed: %s", b.options.InstanceID, msg.Ack.Message)`)
	g.P("}")
	g.P()
	g.P("case *", serviceName, "RegisterResponse_RpcCall:")
	g.P("// Handle incoming RPC call")
	g.P("go b.handleRPCCall(msg.RpcCall)")
	g.P()
	g.P("case *", serviceName, "RegisterResponse_Error:")
	g.P(`log.Printf("[%s] Router error: %s", b.options.InstanceID, msg.Error.Message)`)
	g.P()
	g.P("default:")
	g.P(`log.Printf("[%s] Unknown response type", b.options.InstanceID)`)
	g.P("}")
	g.P("}")
	g.P("}")
	g.P()

	return nil
}

// generateRPCDispatch generates the RPC call handling and dispatch logic
func generateRPCDispatch(g *protogen.GeneratedFile, service *protogen.Service, serviceName, bridgeName string) error {
	// Main RPC handler
	g.P("// handleRPCCall processes incoming RPC calls and dispatches to service methods")
	g.P("func (b *", bridgeName, ") handleRPCCall(rpcCall *", serviceName, "RpcCall) {")
	g.P(`log.Printf("[%s] Handling RPC call: %s (ID: %s)", b.options.InstanceID, rpcCall.Method, rpcCall.RequestId)`)
	g.P()
	g.P("var response *", serviceName, "RpcResponse")
	g.P()
	g.P("// Dispatch based on typed request oneof")
	g.P("switch req := rpcCall.Request.(type) {")

	// Generate cases for each service method
	for _, method := range service.Methods {
		methodName := method.GoName
		g.P("case *", serviceName, "RpcCall_", methodName, ":")
		g.P("response = b.handle", methodName, "(rpcCall.RequestId, req.", methodName, ")")
		g.P()
	}

	g.P("default:")
	g.P(`log.Printf("[%s] Unknown method in RPC call", b.options.InstanceID)`)
	g.P("response = &", serviceName, "RpcResponse{")
	g.P("RequestId: rpcCall.RequestId,")
	g.P("Status: &pb.RpcStatus{")
	g.P("Code:    12, // UNIMPLEMENTED")
	g.P(`Message: "Method not implemented",`)
	g.P("},")
	g.P("Metadata: make(map[string]string),")
	g.P("}")
	g.P("}")
	g.P()
	g.P("// Send response")
	g.P("b.sendResponse(response)")
	g.P("}")
	g.P()

	// Generate individual method handlers
	for _, method := range service.Methods {
		if err := generateMethodHandler(g, method, serviceName, bridgeName); err != nil {
			return err
		}
	}

	// Send response method
	g.P("// sendResponse sends a typed response back to the router")
	g.P("func (b *", bridgeName, ") sendResponse(response *", serviceName, "RpcResponse) {")
	g.P("req := &", serviceName, "RegisterRequest{")
	g.P("Request: &", serviceName, "RegisterRequest_RpcResponse{")
	g.P("RpcResponse: response,")
	g.P("},")
	g.P("}")
	g.P()
	g.P("if err := b.stream.Send(req); err != nil {")
	g.P(`log.Printf("[%s] Failed to send response: %v", b.options.InstanceID, err)`)
	g.P("}")
	g.P("}")
	g.P()

	return nil
}

// generateMethodHandler generates a handler for a specific service method
func generateMethodHandler(g *protogen.GeneratedFile, method *protogen.Method, serviceName, bridgeName string) error {
	methodName := method.GoName
	
	// Only implement unary methods for now, others are TODO
	if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
		// Generate stub for streaming methods
		g.P("// handle", methodName, " handles ", methodName, " RPC calls (streaming - not yet implemented)")
		g.P("func (b *", bridgeName, ") handle", methodName, "(requestID string, req *", method.Input.GoIdent.GoName, ") *", serviceName, "RpcResponse {")
		g.P(`log.Printf("[%s] `, methodName, ` (streaming) not yet implemented", b.options.InstanceID)`)
		g.P("return &", serviceName, "RpcResponse{")
		g.P("RequestId: requestID,")
		g.P("Status: &pb.RpcStatus{")
		g.P("Code:    12, // UNIMPLEMENTED")
		g.P(`Message: "Streaming methods not yet implemented",`)
		g.P("},")
		g.P("Metadata: make(map[string]string),")
		g.P("}")
		g.P("}")
		g.P()
		return nil
	}

	// Generate unary method handler
	g.P("// handle", methodName, " handles ", methodName, " RPC calls")
	g.P("func (b *", bridgeName, ") handle", methodName, "(requestID string, req *", method.Input.GoIdent.GoName, ") *", serviceName, "RpcResponse {")
	g.P(`log.Printf("[%s] Processing `, methodName, ` request", b.options.InstanceID)`)
	g.P()
	g.P("// Call the service method")
	g.P("ctx := context.Background()")
	g.P("resp, err := b.service.", methodName, "(ctx, req)")
	g.P()
	g.P("if err != nil {")
	g.P(`log.Printf("[%s] `, methodName, ` call failed: %v", b.options.InstanceID, err)`)
	g.P("return &", serviceName, "RpcResponse{")
	g.P("RequestId: requestID,")
	g.P("Status: &pb.RpcStatus{")
	g.P("Code:    13, // INTERNAL")
	g.P(`Message: fmt.Sprintf("Service method failed: %v", err),`)
	g.P("},")
	g.P("Metadata: make(map[string]string),")
	g.P("}")
	g.P("}")
	g.P()
	g.P(`log.Printf("[%s] `, methodName, ` completed successfully", b.options.InstanceID)`)
	g.P("return &", serviceName, "RpcResponse{")
	g.P("RequestId: requestID,")
	g.P("Status: &pb.RpcStatus{")
	g.P("Code:    0, // OK")
	g.P(`Message: "Success",`)
	g.P("},")
	g.P("Metadata: make(map[string]string),")
	g.P("Response: &", serviceName, "RpcResponse_", methodName, "{", methodName, ": resp},")
	g.P("}")
	g.P("}")
	g.P()

	return nil
}