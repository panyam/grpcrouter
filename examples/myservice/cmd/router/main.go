package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	myservice "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
	pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
	"github.com/panyam/grpcrouter/router"
)

func main() {
	var (
		port            = flag.String("port", "9080", "Port to listen on")
		timeout         = flag.Duration("timeout", 30*time.Second, "Default RPC timeout")
		maxConcurrent   = flag.Int("max-concurrent", 1000, "Maximum concurrent RPCs")
		healthInterval  = flag.Duration("health-interval", 30*time.Second, "Health check interval")
		cleanupInterval = flag.Duration("cleanup-interval", 60*time.Second, "Stale instance cleanup interval")
	)
	flag.Parse()

	log.Printf("Starting MyService Router on port %s", *port)

	// Create router configuration
	config := &router.RouterConfig{
		DefaultTimeout:      *timeout,
		MaxConcurrentRPCs:   *maxConcurrent,
		HealthCheckInterval: *healthInterval,
	}

	// Create MyService-specific router
	myServiceRouter := myservice.NewMyServiceRouter(config)

	// Create generic router server for registration handling
	genericRouter := router.NewRouterServer(config)

	// Create gRPC server
	grpcServer := grpc.NewServer(
		grpc.MaxConcurrentStreams(uint32(*maxConcurrent)),
	)

	// Register both the generic router (for registration) and MyService router (for service calls)
	pb.RegisterRouterServer(grpcServer, genericRouter)
	myservice.RegisterMyServiceServer(grpcServer, myServiceRouter)

	// Enable reflection for debugging
	reflection.Register(grpcServer)

	// Create listener
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", *port, err)
	}

	log.Printf("MyService Router configuration:")
	log.Printf("  - Timeout: %v", *timeout)
	log.Printf("  - Max Concurrent RPCs: %d", *maxConcurrent)
	log.Printf("  - Health Check Interval: %v", *healthInterval)
	log.Printf("  - Cleanup Interval: %v", *cleanupInterval)

	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(*cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Cleanup stale instances (2x health check interval)
				log.Printf("Running cleanup for stale instances...")
				genericRouter.CleanupStaleInstances(*healthInterval * 2)

			case <-context.Background().Done():
				return
			}
		}
	}()

	// Start stats reporting goroutine
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// TODO: Add stats reporting when registry stats are accessible
				log.Printf("Router stats: Active connections and load balancing info would go here")

			case <-context.Background().Done():
				return
			}
		}
	}()

	// Start server in goroutine
	go func() {
		log.Printf("MyService Router listening on :%s", *port)
		log.Printf("Available endpoints:")
		log.Printf("  - Router.Register (for service instance registration)")
		log.Printf("  - MyService.Method1 (unary RPC)")
		log.Printf("  - MyService.Method2 (server streaming)")
		log.Printf("  - MyService.Method3 (client streaming)")
		log.Printf("  - MyService.StreamMethod (bidirectional streaming)")

		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt signal
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	log.Println("Shutting down MyService Router...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a channel to signal when graceful stop is complete
	stopped := make(chan bool, 1)
	go func() {
		grpcServer.GracefulStop()
		stopped <- true
	}()

	// Wait for graceful stop or timeout
	select {
	case <-stopped:
		log.Println("MyService Router stopped gracefully")
	case <-ctx.Done():
		log.Println("Graceful shutdown timeout, forcing stop")
		grpcServer.Stop()
	}
}
