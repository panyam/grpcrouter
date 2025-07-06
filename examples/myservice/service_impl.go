package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	myservice "github.com/panyam/grpcrouter/examples/myservice/gen/go/myservice/v1"
)

// MyServiceImpl implements the MyService interface with instance ID tracking
type MyServiceImpl struct {
	myservice.UnimplementedMyServiceServer
	
	// Instance identification
	InstanceID string
	
	// Internal state for demonstration
	mu           sync.RWMutex
	requestCount int64
}

// NewMyServiceImpl creates a new MyService implementation
func NewMyServiceImpl(instanceID string) *MyServiceImpl {
	return &MyServiceImpl{
		InstanceID: instanceID,
	}
}

// Method1 implements the unary RPC method
func (s *MyServiceImpl) Method1(ctx context.Context, req *myservice.Method1Request) (*myservice.Method1Response, error) {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()
	
	log.Printf("[%s] Method1 called with data: %s (request #%d)", s.InstanceID, req.Data, count)
	
	// Simulate some processing time
	time.Sleep(10 * time.Millisecond)
	
	result := fmt.Sprintf("[%s] Processed: %s (value: %d, request: #%d)", 
		s.InstanceID, req.Data, req.Value, count)
	
	return &myservice.Method1Response{
		Result:     result,
		InstanceId: s.InstanceID,
	}, nil
}

// Method2 implements the server streaming RPC method
func (s *MyServiceImpl) Method2(req *myservice.Method2Request, stream myservice.MyService_Method2Server) error {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()
	
	log.Printf("[%s] Method2 called with %d items (request #%d)", s.InstanceID, len(req.Items), count)
	
	for i, item := range req.Items {
		// Simulate processing time for each item
		time.Sleep(50 * time.Millisecond)
		
		result := fmt.Sprintf("[%s] Item %d: %s (processed at %s)", 
			s.InstanceID, i, item, time.Now().Format("15:04:05.000"))
		
		response := &myservice.Method2Response{
			Item:       item,
			Result:     result,
			InstanceId: s.InstanceID,
		}
		
		if err := stream.Send(response); err != nil {
			log.Printf("[%s] Failed to send response: %v", s.InstanceID, err)
			return err
		}
		
		log.Printf("[%s] Sent response for item %d: %s", s.InstanceID, i, item)
	}
	
	log.Printf("[%s] Method2 completed for request #%d", s.InstanceID, count)
	return nil
}

// Method3 implements the client streaming RPC method
func (s *MyServiceImpl) Method3(stream myservice.MyService_Method3Server) error {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()
	
	log.Printf("[%s] Method3 started (request #%d)", s.InstanceID, count)
	
	var totalProcessed int32
	var messages []string
	
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			// Client finished sending
			break
		}
		if err != nil {
			log.Printf("[%s] Error receiving stream: %v", s.InstanceID, err)
			return err
		}
		
		totalProcessed++
		messages = append(messages, fmt.Sprintf("[%s] Batch %d: %s", 
			s.InstanceID, req.BatchId, req.Data))
		
		log.Printf("[%s] Received message %d: batch=%d, data=%s", 
			s.InstanceID, totalProcessed, req.BatchId, req.Data)
		
		// Simulate processing time
		time.Sleep(20 * time.Millisecond)
	}
	
	summary := fmt.Sprintf("[%s] Processed %d messages for request #%d", 
		s.InstanceID, totalProcessed, count)
	
	response := &myservice.Method3Response{
		TotalProcessed: totalProcessed,
		Summary:        summary,
		InstanceId:     s.InstanceID,
	}
	
	log.Printf("[%s] Method3 completed: %s", s.InstanceID, summary)
	return stream.SendAndClose(response)
}

// StreamMethod implements the bidirectional streaming RPC method
func (s *MyServiceImpl) StreamMethod(stream myservice.MyService_StreamMethodServer) error {
	s.mu.Lock()
	s.requestCount++
	count := s.requestCount
	s.mu.Unlock()
	
	log.Printf("[%s] StreamMethod started (request #%d)", s.InstanceID, count)
	
	// Use channels to coordinate between receive and send goroutines
	requestChan := make(chan *myservice.StreamRequest, 10)
	errorChan := make(chan error, 1)
	done := make(chan bool, 1)
	
	// Goroutine to receive messages from client
	go func() {
		defer close(requestChan)
		for {
			req, err := stream.Recv()
			if err == io.EOF {
				log.Printf("[%s] Client closed stream", s.InstanceID)
				return
			}
			if err != nil {
				log.Printf("[%s] Error receiving stream: %v", s.InstanceID, err)
				errorChan <- err
				return
			}
			
			log.Printf("[%s] Received stream message: seq=%d, msg=%s", 
				s.InstanceID, req.Sequence, req.Message)
			
			requestChan <- req
		}
	}()
	
	// Goroutine to send responses back to client
	go func() {
		defer func() { done <- true }()
		
		for req := range requestChan {
			// Process the request and create response
			time.Sleep(30 * time.Millisecond) // Simulate processing
			
			response := &myservice.StreamResponse{
				Response:   fmt.Sprintf("[%s] Echo: %s (processed seq %d)", s.InstanceID, req.Message, req.Sequence),
				Sequence:   req.Sequence,
				InstanceId: s.InstanceID,
			}
			
			if err := stream.Send(response); err != nil {
				log.Printf("[%s] Error sending response: %v", s.InstanceID, err)
				errorChan <- err
				return
			}
			
			log.Printf("[%s] Sent response: seq=%d", s.InstanceID, req.Sequence)
		}
	}()
	
	// Wait for completion or error
	select {
	case err := <-errorChan:
		log.Printf("[%s] StreamMethod error: %v", s.InstanceID, err)
		return err
	case <-done:
		log.Printf("[%s] StreamMethod completed for request #%d", s.InstanceID, count)
		return nil
	case <-stream.Context().Done():
		log.Printf("[%s] StreamMethod cancelled", s.InstanceID)
		return stream.Context().Err()
	}
}

// GetStats returns instance statistics (bonus method for debugging)
func (s *MyServiceImpl) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	return map[string]interface{}{
		"instance_id":    s.InstanceID,
		"request_count":  s.requestCount,
		"uptime":         time.Since(time.Now()), // This would be tracked properly in real impl
	}
}