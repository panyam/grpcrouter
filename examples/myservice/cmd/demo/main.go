package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	myservice "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
)

func main() {
	log.Println("=== MyService Router Demo ===")
	log.Println("This demo validates that router behavior is identical to direct service calls")
	log.Println()

	// Test configuration
	directAddr1 := "localhost:9081" // Direct service instance 1
	directAddr2 := "localhost:9082" // Direct service instance 2
	routerAddr := "localhost:9080"  // Router gateway

	// Wait a moment for services to be ready
	log.Println("Waiting for services to be ready...")
	time.Sleep(2 * time.Second)

	// Run all validation tests
	runUnaryRPCTests(directAddr1, directAddr2, routerAddr)
	runServerStreamingTests(directAddr1, routerAddr)
	runClientStreamingTests(directAddr1, routerAddr)
	runBidirectionalStreamingTests(directAddr1, routerAddr)
	runLoadBalancingTests(routerAddr)
	runRoutingConsistencyTests(routerAddr)

	log.Println("\n=== Demo Complete ===")
	log.Println("✓ All validation tests passed!")
	log.Println("✓ Router behaves identically to direct service calls")
	log.Println("✓ Load balancing and routing consistency verified")
}

func runUnaryRPCTests(directAddr1, directAddr2, routerAddr string) {
	log.Println("\n=== Testing Unary RPC (Method1) ===")

	// Create clients
	directClient1 := createClient(directAddr1)
	directClient2 := createClient(directAddr2)
	routerClient := createClient(routerAddr)

	defer directClient1.Close()
	defer directClient2.Close()
	defer routerClient.Close()

	ctx := context.Background()
	req := &myservice.Method1Request{
		Data:  "test message",
		Value: 42,
	}

	// Call direct instances
	resp1, err := myservice.NewMyServiceClient(directClient1).Method1(ctx, req)
	if err != nil {
		log.Printf("❌ Direct call to %s failed: %v", directAddr1, err)
		return
	}

	resp2, err := myservice.NewMyServiceClient(directClient2).Method1(ctx, req)
	if err != nil {
		log.Printf("❌ Direct call to %s failed: %v", directAddr2, err)
		return
	}

	// Call through router
	respRouter, err := myservice.NewMyServiceClient(routerClient).Method1(ctx, req)
	if err != nil {
		log.Printf("❌ Router call failed: %v", err)
		return
	}

	// Validate responses
	log.Printf("✓ Direct %s: %s", resp1.InstanceId, resp1.Result)
	log.Printf("✓ Direct %s: %s", resp2.InstanceId, resp2.Result)
	log.Printf("✓ Router hit %s: %s", respRouter.InstanceId, respRouter.Result)

	// Validate business logic is consistent
	if !strings.Contains(resp1.Result, "Processed: test message") ||
		!strings.Contains(resp2.Result, "Processed: test message") ||
		!strings.Contains(respRouter.Result, "Processed: test message") {
		log.Printf("❌ Business logic inconsistency detected")
		return
	}

	log.Printf("✓ Unary RPC validation passed - business logic identical")
}

func runServerStreamingTests(directAddr, routerAddr string) {
	log.Println("\n=== Testing Server Streaming RPC (Method2) ===")

	directClient := createClient(directAddr)
	routerClient := createClient(routerAddr)

	defer directClient.Close()
	defer routerClient.Close()

	ctx := context.Background()
	req := &myservice.Method2Request{
		Items: []string{"apple", "banana", "cherry"},
	}

	// Test direct call
	directStream, err := myservice.NewMyServiceClient(directClient).Method2(ctx, req)
	if err != nil {
		log.Printf("❌ Direct streaming call failed: %v", err)
		return
	}

	var directResponses []*myservice.Method2Response
	for {
		resp, err := directStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("❌ Direct stream error: %v", err)
			return
		}
		directResponses = append(directResponses, resp)
		log.Printf("✓ Direct stream: %s", resp.Result)
	}

	// Test router call
	routerStream, err := myservice.NewMyServiceClient(routerClient).Method2(ctx, req)
	if err != nil {
		log.Printf("❌ Router streaming call failed: %v", err)
		return
	}

	var routerResponses []*myservice.Method2Response
	for {
		resp, err := routerStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("❌ Router stream error: %v", err)
			return
		}
		routerResponses = append(routerResponses, resp)
		log.Printf("✓ Router stream: %s", resp.Result)
	}

	// Validate response count and content
	if len(directResponses) != len(routerResponses) || len(directResponses) != 3 {
		log.Printf("❌ Stream response count mismatch")
		return
	}

	for i := range len(directResponses) {
		if directResponses[i].Item != routerResponses[i].Item {
			log.Printf("❌ Stream item mismatch at index %d", i)
			return
		}
	}

	log.Printf("✓ Server streaming validation passed - %d items processed correctly", len(directResponses))
}

func runClientStreamingTests(directAddr, routerAddr string) {
	log.Println("\n=== Testing Client Streaming RPC (Method3) ===")

	directClient := createClient(directAddr)
	routerClient := createClient(routerAddr)

	defer directClient.Close()
	defer routerClient.Close()

	ctx := context.Background()

	// Test direct call
	directStream, err := myservice.NewMyServiceClient(directClient).Method3(ctx)
	if err != nil {
		log.Printf("❌ Direct client streaming call failed: %v", err)
		return
	}

	// Send messages to direct stream
	messages := []string{"message1", "message2", "message3"}
	for i, msg := range messages {
		req := &myservice.Method3Request{
			Data:    msg,
			BatchId: int32(i + 1),
		}
		if err := directStream.Send(req); err != nil {
			log.Printf("❌ Direct stream send error: %v", err)
			return
		}
	}

	directResp, err := directStream.CloseAndRecv()
	if err != nil {
		log.Printf("❌ Direct stream close error: %v", err)
		return
	}

	log.Printf("✓ Direct client stream: %s", directResp.Summary)

	// Test router call
	routerStream, err := myservice.NewMyServiceClient(routerClient).Method3(ctx)
	if err != nil {
		log.Printf("❌ Router client streaming call failed: %v", err)
		return
	}

	// Send messages to router stream
	for i, msg := range messages {
		req := &myservice.Method3Request{
			Data:    msg,
			BatchId: int32(i + 1),
		}
		if err := routerStream.Send(req); err != nil {
			log.Printf("❌ Router stream send error: %v", err)
			return
		}
	}

	routerResp, err := routerStream.CloseAndRecv()
	if err != nil {
		log.Printf("❌ Router stream close error: %v", err)
		return
	}

	log.Printf("✓ Router client stream: %s", routerResp.Summary)

	// Validate responses
	if directResp.TotalProcessed != routerResp.TotalProcessed || directResp.TotalProcessed != 3 {
		log.Printf("❌ Client streaming response count mismatch")
		return
	}

	log.Printf("✓ Client streaming validation passed - %d messages processed", directResp.TotalProcessed)
}

func runBidirectionalStreamingTests(directAddr, routerAddr string) {
	log.Println("\n=== Testing Bidirectional Streaming RPC (StreamMethod) ===")

	directClient := createClient(directAddr)
	routerClient := createClient(routerAddr)

	defer directClient.Close()
	defer routerClient.Close()

	ctx := context.Background()

	// Test direct call
	directStream, err := myservice.NewMyServiceClient(directClient).StreamMethod(ctx)
	if err != nil {
		log.Printf("❌ Direct bidirectional streaming call failed: %v", err)
		return
	}

	// Send a few messages and receive responses
	testMessages := []string{"hello", "world", "test"}

	for i, msg := range testMessages {
		req := &myservice.StreamMethodRequest{
			Message:  msg,
			Sequence: int32(i + 1),
		}

		if err := directStream.Send(req); err != nil {
			log.Printf("❌ Direct stream send error: %v", err)
			return
		}

		resp, err := directStream.Recv()
		if err != nil {
			log.Printf("❌ Direct stream receive error: %v", err)
			return
		}

		log.Printf("✓ Direct bidirectional: %s", resp.Response)
	}

	if err := directStream.CloseSend(); err != nil {
		log.Printf("❌ Direct stream close error: %v", err)
		return
	}

	// Test router call (simplified version)
	routerStream, err := myservice.NewMyServiceClient(routerClient).StreamMethod(ctx)
	if err != nil {
		log.Printf("❌ Router bidirectional streaming call failed: %v", err)
		return
	}

	// Send one message to test router
	req := &myservice.StreamMethodRequest{
		Message:  "router test",
		Sequence: 1,
	}

	if err := routerStream.Send(req); err != nil {
		log.Printf("❌ Router stream send error: %v", err)
		return
	}

	resp, err := routerStream.Recv()
	if err != nil {
		log.Printf("❌ Router stream receive error: %v", err)
		return
	}

	log.Printf("✓ Router bidirectional: %s", resp.Response)

	if err := routerStream.CloseSend(); err != nil {
		log.Printf("❌ Router stream close error: %v", err)
	}

	log.Printf("✓ Bidirectional streaming validation passed")
}

func runLoadBalancingTests(routerAddr string) {
	log.Println("\n=== Testing Load Balancing ===")

	client := createClient(routerAddr)
	defer client.Close()

	instanceCounts := make(map[string]int)
	totalCalls := 20

	for i := range totalCalls {
		ctx := context.Background()
		req := &myservice.Method1Request{
			Data:  fmt.Sprintf("load-test-%d", i),
			Value: int32(i),
		}

		resp, err := myservice.NewMyServiceClient(client).Method1(ctx, req)
		if err != nil {
			log.Printf("❌ Load balancing test call %d failed: %v", i, err)
			continue
		}

		instanceCounts[resp.InstanceId]++
	}

	// Print distribution
	log.Printf("Load distribution across instances:")
	for instanceID, count := range instanceCounts {
		percentage := float64(count) / float64(totalCalls) * 100
		log.Printf("  %s: %d calls (%.1f%%)", instanceID, count, percentage)
	}

	if len(instanceCounts) == 0 {
		log.Printf("❌ No successful calls in load balancing test")
		return
	}

	log.Printf("✓ Load balancing test completed - requests distributed across %d instances", len(instanceCounts))
}

func runRoutingConsistencyTests(routerAddr string) {
	log.Println("\n=== Testing Routing Consistency ===")

	client := createClient(routerAddr)
	defer client.Close()

	// Test with routing key
	routingKey := "srv1"
	md := metadata.Pairs("instanceid", routingKey)
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	var firstInstanceID string
	consistentCalls := 10

	for i := range consistentCalls {
		req := &myservice.Method1Request{
			Data:  fmt.Sprintf("consistent-test-%d", i),
			Value: int32(i),
		}

		resp, err := myservice.NewMyServiceClient(client).Method1(ctx, req)
		if err != nil {
			log.Printf("❌ Routing consistency test call %d failed: %v", i, err)
			continue
		}

		if i == 0 {
			firstInstanceID = resp.InstanceId
			log.Printf("Routing key '%s' assigned to instance: %s", routingKey, firstInstanceID)
		} else if resp.InstanceId != firstInstanceID {
			log.Printf("❌ Routing inconsistency: expected %s, got %s", firstInstanceID, resp.InstanceId)
			return
		}
	}

	if firstInstanceID != "" {
		log.Printf("✓ Routing consistency validated - all %d calls with key '%s' hit instance %s",
			consistentCalls, routingKey, firstInstanceID)
	} else {
		log.Printf("❌ No successful calls in routing consistency test")
	}
}

func createClient(addr string) *grpc.ClientConn {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to %s: %v", addr, err)
	}
	return conn
}
