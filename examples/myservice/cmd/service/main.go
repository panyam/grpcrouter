package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	impl "github.com/panyam/grpcrouter/examples/myservice"
	myservice "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
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

	log.Printf("Starting MyService instance")
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

	log.Printf("MyService instance running in DIRECT mode on port %s", port)

	// Start server in goroutine
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt
	waitForInterrupt()

	log.Printf("Shutting down instance...")
	server.GracefulStop()
}

// runRouterMode connects the service to a router using the auto-generated bridge
func runRouterMode(service *impl.MyServiceImpl, routerAddr, instanceID, endpoint string) {
	// Configure bridge options
	options := myservice.DefaultMyServiceBridgeOptions()
	options.RouterAddr = routerAddr
	
	// Use provided values or auto-generate
	if instanceID != "" {
		options.InstanceID = instanceID
	}
	if endpoint != "" {
		options.Endpoint = endpoint
	}
	
	// Add custom metadata
	options.Metadata["version"] = "1.0.0"
	options.Metadata["region"] = "local"

	log.Printf("MyService instance %s connecting to MyServiceRouter at %s", options.InstanceID, routerAddr)

	// Create and start the auto-generated bridge
	bridge := myservice.NewMyServiceBridge(service, options)

	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start bridge: %v", err)
	}

	log.Printf("MyService instance %s running in ROUTER mode via auto-generated bridge", options.InstanceID)

	// Wait for interrupt
	waitForInterrupt()

	// Graceful shutdown
	log.Printf("Shutting down bridge...")
	bridge.Stop()
	bridge.Wait()
	log.Printf("Bridge stopped")
}

// waitForInterrupt waits for an interrupt signal
func waitForInterrupt() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
}