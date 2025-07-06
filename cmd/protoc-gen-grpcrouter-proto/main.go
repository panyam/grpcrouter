package main

import (
	"fmt"
	"log"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		gen.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)

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
	})
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