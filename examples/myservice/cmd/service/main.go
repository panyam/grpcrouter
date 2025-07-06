package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/types/known/timestamppb"

	impl "github.com/panyam/grpcrouter/examples/myservice"
	myservice "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
	pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

func main() {
	var (
		mode     = flag.String("mode", "direct", "Service mode: 'direct' or 'router'")
		port     = flag.String("port", "9081", "Port for direct mode")
		router   = flag.String("router", "localhost:9080", "Router address for router mode")
		id       = flag.String("id", "", "Instance ID (auto-generated if empty)")
		endpoint = flag.String("endpoint", "", "Service endpoint (auto-generated if empty)")
	)
	flag.Parse()

	// Auto-generate instance ID if not provided
	if *id == "" {
		*id = fmt.Sprintf("srv-%d", time.Now().Unix()%10000)
	}

	// Auto-generate endpoint if not provided
	if *endpoint == "" {
		*endpoint = fmt.Sprintf("localhost:%s", *port)
	}

	log.Printf("Starting MyService instance: %s", *id)
	log.Printf("Mode: %s", *mode)

	// Create service implementation
	service := impl.NewMyServiceImpl(*id)

	switch *mode {
	case "direct":
		runDirectMode(service, *port, *id)
	case "router":
		runRouterMode(service, *router, *id, *endpoint)
	default:
		log.Fatalf("Invalid mode: %s. Use 'direct' or 'router'", *mode)
	}
}

// runDirectMode starts the service as a standalone gRPC server
func runDirectMode(service *impl.MyServiceImpl, port, instanceID string) {
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	server := grpc.NewServer()
	myservice.RegisterMyServiceServer(server, service)

	// Enable reflection for debugging
	reflection.Register(server)

	log.Printf("MyService instance %s running in DIRECT mode on port %s", instanceID, port)

	// Start server in goroutine
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt
	waitForInterrupt()

	log.Printf("Shutting down instance %s...", instanceID)
	server.GracefulStop()
}

// runRouterMode connects the service to a MyServiceRouter and handles requests via typed streams
func runRouterMode(service *impl.MyServiceImpl, routerAddr, instanceID, endpoint string) {
	// Connect to MyServiceRouter (not generic router)
	conn, err := grpc.Dial(routerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to router at %s: %v", routerAddr, err)
	}
	defer conn.Close()

	client := myservice.NewMyServiceRouterClient(conn)

	log.Printf("MyService instance %s connecting to MyServiceRouter at %s", instanceID, routerAddr)

	// Create typed registration stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.Register(ctx)
	if err != nil {
		log.Fatalf("Failed to create registration stream: %v", err)
	}

	// Send initial registration with typed message
	registrationReq := &myservice.MyServiceRegisterRequest{
		Request: &myservice.MyServiceRegisterRequest_InstanceInfo{
			InstanceInfo: &pb.InstanceInfo{
				InstanceId:  instanceID,
				ServiceName: "MyService",
				Endpoint:    endpoint,
				Metadata: map[string]string{
					"version":     "1.0.0",
					"region":      "local",
					"routing_key": instanceID, // Use instance ID as routing key
				},
				HealthStatus: pb.HealthStatus_HEALTHY,
				RegisteredAt: timestamppb.Now(),
			},
		},
	}

	if err := stream.Send(registrationReq); err != nil {
		log.Fatalf("Failed to send registration: %v", err)
	}

	log.Printf("MyService instance %s registered with MyServiceRouter in ROUTER mode", instanceID)

	// Start heartbeat goroutine
	go sendTypedHeartbeats(stream, instanceID)

	// Handle incoming typed RPC calls from router
	handleTypedRegistrationResponses(stream, service, instanceID)
}

// sendTypedHeartbeats sends periodic heartbeat messages to the typed router
func sendTypedHeartbeats(stream myservice.MyServiceRouter_RegisterClient, instanceID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeat := &myservice.MyServiceRegisterRequest{
				Request: &myservice.MyServiceRegisterRequest_Heartbeat{
					Heartbeat: &pb.Heartbeat{
						InstanceId:   instanceID,
						Timestamp:    timestamppb.Now(),
						HealthStatus: pb.HealthStatus_HEALTHY,
					},
				},
			}

			if err := stream.Send(heartbeat); err != nil {
				log.Printf("[%s] Failed to send heartbeat: %v", instanceID, err)
				return
			}

			log.Printf("[%s] Sent heartbeat", instanceID)

		case <-stream.Context().Done():
			return
		}
	}
}

// handleTypedRegistrationResponses processes typed messages from the MyServiceRouter
func handleTypedRegistrationResponses(stream myservice.MyServiceRouter_RegisterClient, service *impl.MyServiceImpl, instanceID string) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			log.Printf("[%s] Registration stream error: %v", instanceID, err)
			return
		}

		switch msg := resp.Response.(type) {
		case *myservice.MyServiceRegisterResponse_Ack:
			if msg.Ack.Success {
				log.Printf("[%s] Registration acknowledged: %s", instanceID, msg.Ack.Message)
			} else {
				log.Printf("[%s] Registration failed: %s", instanceID, msg.Ack.Message)
			}

		case *myservice.MyServiceRegisterResponse_RpcCall:
			// Handle incoming typed RPC call
			go handleTypedRPCCall(stream, service, instanceID, msg.RpcCall)

		case *myservice.MyServiceRegisterResponse_Error:
			log.Printf("[%s] Router error: %s", instanceID, msg.Error.Message)

		default:
			log.Printf("[%s] Unknown response type", instanceID)
		}
	}
}

// handleTypedRPCCall processes a single typed RPC call from the MyServiceRouter
func handleTypedRPCCall(stream myservice.MyServiceRouter_RegisterClient, service *impl.MyServiceImpl, instanceID string, rpcCall *myservice.MyServiceRpcCall) {
	log.Printf("[%s] Handling typed RPC call: %s (ID: %s)", instanceID, rpcCall.Method, rpcCall.RequestId)

	var rpcResponse *myservice.MyServiceRpcResponse

	// Handle different typed RPC methods using oneof
	switch req := rpcCall.Request.(type) {
	case *myservice.MyServiceRpcCall_Method1:
		rpcResponse = handleTypedMethod1(service, instanceID, rpcCall.RequestId, req.Method1)
	
	case *myservice.MyServiceRpcCall_Method2:
		rpcResponse = handleTypedMethod2(service, instanceID, rpcCall.RequestId, req.Method2)
	
	case *myservice.MyServiceRpcCall_Method3:
		rpcResponse = handleTypedMethod3(service, instanceID, rpcCall.RequestId, req.Method3)
	
	case *myservice.MyServiceRpcCall_StreamMethod:
		rpcResponse = handleTypedStreamMethod(service, instanceID, rpcCall.RequestId, req.StreamMethod)
	
	default:
		log.Printf("[%s] Unknown typed method in RPC call", instanceID)
		rpcResponse = &myservice.MyServiceRpcResponse{
			RequestId: rpcCall.RequestId,
			Status: &pb.RpcStatus{
				Code:    12, // UNIMPLEMENTED
				Message: "Method not implemented",
			},
			Metadata: make(map[string]string),
		}
	}

	// Send typed response back through registration stream
	response := &myservice.MyServiceRegisterRequest{
		Request: &myservice.MyServiceRegisterRequest_RpcResponse{
			RpcResponse: rpcResponse,
		},
	}

	if err := stream.Send(response); err != nil {
		log.Printf("[%s] Failed to send typed RPC response: %v", instanceID, err)
	}

	log.Printf("[%s] Sent typed response for RPC call %s", instanceID, rpcCall.RequestId)
}

// handleTypedMethod1 processes typed Method1 RPC calls
func handleTypedMethod1(service *impl.MyServiceImpl, instanceID, requestID string, req *myservice.Method1Request) *myservice.MyServiceRpcResponse {
	log.Printf("[%s] Processing Method1 request", instanceID)
	
	// Call the actual service method
	ctx := context.Background()
	resp, err := service.Method1(ctx, req)
	
	if err != nil {
		log.Printf("[%s] Method1 call failed: %v", instanceID, err)
		return &myservice.MyServiceRpcResponse{
			RequestId: requestID,
			Status: &pb.RpcStatus{
				Code:    13, // INTERNAL
				Message: fmt.Sprintf("Service method failed: %v", err),
			},
			Metadata: make(map[string]string),
		}
	}

	log.Printf("[%s] Method1 completed successfully", instanceID)
	return &myservice.MyServiceRpcResponse{
		RequestId: requestID,
		Status: &pb.RpcStatus{
			Code:    0, // OK
			Message: "Success",
		},
		Metadata: make(map[string]string),
		Response: &myservice.MyServiceRpcResponse_Method1{Method1: resp},
	}
}

// handleTypedMethod2 processes typed Method2 RPC calls (server streaming)
func handleTypedMethod2(service *impl.MyServiceImpl, instanceID, requestID string, req *myservice.Method2Request) *myservice.MyServiceRpcResponse {
	log.Printf("[%s] Method2 (server streaming) not yet implemented", instanceID)
	return &myservice.MyServiceRpcResponse{
		RequestId: requestID,
		Status: &pb.RpcStatus{
			Code:    12, // UNIMPLEMENTED
			Message: "Server streaming not yet implemented",
		},
		Metadata: make(map[string]string),
	}
}

// handleTypedMethod3 processes typed Method3 RPC calls (client streaming)
func handleTypedMethod3(service *impl.MyServiceImpl, instanceID, requestID string, req *myservice.Method3Request) *myservice.MyServiceRpcResponse {
	log.Printf("[%s] Method3 (client streaming) not yet implemented", instanceID)
	return &myservice.MyServiceRpcResponse{
		RequestId: requestID,
		Status: &pb.RpcStatus{
			Code:    12, // UNIMPLEMENTED
			Message: "Client streaming not yet implemented",
		},
		Metadata: make(map[string]string),
	}
}

// handleTypedStreamMethod processes typed StreamMethod RPC calls (bidirectional streaming)
func handleTypedStreamMethod(service *impl.MyServiceImpl, instanceID, requestID string, req *myservice.StreamMethodRequest) *myservice.MyServiceRpcResponse {
	log.Printf("[%s] StreamMethod (bidirectional streaming) not yet implemented", instanceID)
	return &myservice.MyServiceRpcResponse{
		RequestId: requestID,
		Status: &pb.RpcStatus{
			Code:    12, // UNIMPLEMENTED
			Message: "Bidirectional streaming not yet implemented",
		},
		Metadata: make(map[string]string),
	}
}

// waitForInterrupt waits for an interrupt signal
func waitForInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
