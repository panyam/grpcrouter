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

	"github.com/panyam/grpcrouter/examples/myservice"
	pb "github.com/panyam/grpcrouter/examples/myservice/gen/go/grpcrouter/v1"
	myservicev1 "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
)

func main() {
	var (
		mode     = flag.String("mode", "direct", "Service mode: 'direct' or 'router'")
		port     = flag.String("port", "8081", "Port for direct mode")
		router   = flag.String("router", "localhost:8080", "Router address for router mode")
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
	service := myservice.NewMyServiceImpl(*id)

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
func runDirectMode(service *myservice.MyServiceImpl, port, instanceID string) {
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}

	server := grpc.NewServer()
	myservicev1.RegisterMyServiceServer(server, service)

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

// runRouterMode connects the service to a router and handles requests via streams
func runRouterMode(service *myservice.MyServiceImpl, routerAddr, instanceID, endpoint string) {
	// Connect to router
	conn, err := grpc.Dial(routerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to router at %s: %v", routerAddr, err)
	}
	defer conn.Close()

	client := pb.NewRouterClient(conn)

	log.Printf("MyService instance %s connecting to router at %s", instanceID, routerAddr)

	// Create registration stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := client.Register(ctx)
	if err != nil {
		log.Fatalf("Failed to create registration stream: %v", err)
	}

	// Send initial registration
	registrationReq := &pb.RegisterRequest{
		Request: &pb.RegisterRequest_InstanceInfo{
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

	log.Printf("MyService instance %s registered with router in ROUTER mode", instanceID)

	// Start heartbeat goroutine
	go sendHeartbeats(stream, instanceID)

	// Start RPC handler goroutine
	go handleRPCCalls(stream, service, instanceID)

	// Handle registration responses
	handleRegistrationResponses(stream, service, instanceID)
}

// sendHeartbeats sends periodic heartbeat messages to the router
func sendHeartbeats(stream pb.Router_RegisterClient, instanceID string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeat := &pb.RegisterRequest{
				Request: &pb.RegisterRequest_Heartbeat{
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

// handleRegistrationResponses processes messages from the router
func handleRegistrationResponses(stream pb.Router_RegisterClient, service *myservice.MyServiceImpl, instanceID string) {
	for {
		resp, err := stream.Recv()
		if err != nil {
			log.Printf("[%s] Registration stream error: %v", instanceID, err)
			return
		}

		switch msg := resp.Response.(type) {
		case *pb.RegisterResponse_Ack:
			if msg.Ack.Success {
				log.Printf("[%s] Registration acknowledged: %s", instanceID, msg.Ack.Message)
			} else {
				log.Printf("[%s] Registration failed: %s", instanceID, msg.Ack.Message)
			}

		case *pb.RegisterResponse_RpcCall:
			// Handle incoming RPC call
			go handleSingleRPCCall(stream, service, instanceID, msg.RpcCall)

		case *pb.RegisterResponse_Error:
			log.Printf("[%s] Router error: %s", instanceID, msg.Error.Message)

		default:
			log.Printf("[%s] Unknown response type", instanceID)
		}
	}
}

// handleRPCCalls is a placeholder for handling RPC calls (actual implementation would be more complex)
func handleRPCCalls(stream pb.Router_RegisterClient, service *myservice.MyServiceImpl, instanceID string) {
	// This would contain the logic to handle different types of RPC calls
	// For now, we'll rely on handleSingleRPCCall for each incoming call
	log.Printf("[%s] RPC handler goroutine started", instanceID)
}

// handleSingleRPCCall processes a single RPC call from the router
func handleSingleRPCCall(stream pb.Router_RegisterClient, service *myservice.MyServiceImpl, instanceID string, rpcCall *pb.RpcCall) {
	log.Printf("[%s] Handling RPC call: %s (ID: %s)", instanceID, rpcCall.Method, rpcCall.RequestId)

	// This is a simplified implementation - a real implementation would:
	// 1. Parse the method name to determine which service method to call
	// 2. Unmarshal the request from Any type to the specific request type
	// 3. Call the appropriate service method
	// 4. Marshal the response back to Any type
	// 5. Send the response back through the stream

	// For now, we'll just acknowledge that we received the call
	response := &pb.RegisterRequest{
		Request: &pb.RegisterRequest_RpcResponse{
			RpcResponse: &pb.RpcResponse{
				RequestId: rpcCall.RequestId,
				Status: &pb.RpcStatus{
					Code:    0, // OK
					Message: "Call handled successfully",
				},
			},
		},
	}

	if err := stream.Send(response); err != nil {
		log.Printf("[%s] Failed to send RPC response: %v", instanceID, err)
	}

	log.Printf("[%s] Sent response for RPC call %s", instanceID, rpcCall.RequestId)
}

// waitForInterrupt waits for an interrupt signal
func waitForInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}
