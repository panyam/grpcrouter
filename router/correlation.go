package router

import (
	"context"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"

	pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

// RequestCorrelator manages pending RPC requests across the gateway
type RequestCorrelator struct {
	mu           sync.RWMutex
	pendingCalls map[string]*PendingCall
	idGenerator  func() string
}

// PendingCall represents an active RPC call waiting for response
type PendingCall struct {
	RequestID  string
	Method     string
	MethodType pb.RpcMethodType
	Context    context.Context
	Cancel     context.CancelFunc
	CreatedAt  time.Time
	Timeout    time.Duration

	// Response channels for different RPC types
	ResponseChan chan *anypb.Any        // For unary and client streaming
	StreamChan   chan *pb.StreamMessage // For server and bidirectional streaming
	StatusChan   chan *pb.RpcStatus     // For completion status
	ErrorChan    chan error             // For errors

	// Stream state management
	ClientStreamDone bool  // Client finished sending
	ServerStreamDone bool  // Server finished sending
	StreamSequence   int64 // Next expected sequence number
}

// NewRequestCorrelator creates a new request correlator
func NewRequestCorrelator() *RequestCorrelator {
	return &RequestCorrelator{
		pendingCalls: make(map[string]*PendingCall),
		idGenerator:  generateRequestID,
	}
}

// CreatePendingCall creates a new pending call and returns its request ID
func (rc *RequestCorrelator) CreatePendingCall(
	ctx context.Context,
	method string,
	methodType pb.RpcMethodType,
	timeout time.Duration,
) *PendingCall {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	requestID := rc.idGenerator()
	callCtx, cancel := context.WithTimeout(ctx, timeout)

	call := &PendingCall{
		RequestID:      requestID,
		Method:         method,
		MethodType:     methodType,
		Context:        callCtx,
		Cancel:         cancel,
		CreatedAt:      time.Now(),
		Timeout:        timeout,
		ResponseChan:   make(chan *anypb.Any, 1),
		StreamChan:     make(chan *pb.StreamMessage, 100), // Buffer for streaming
		StatusChan:     make(chan *pb.RpcStatus, 1),
		ErrorChan:      make(chan error, 1),
		StreamSequence: 1,
	}

	rc.pendingCalls[requestID] = call

	// Start timeout handler
	go rc.handleTimeout(call)

	return call
}

// GetPendingCall retrieves a pending call by request ID
func (rc *RequestCorrelator) GetPendingCall(requestID string) (*PendingCall, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	call, exists := rc.pendingCalls[requestID]
	return call, exists
}

// HandleResponse processes an RPC response and routes it to the appropriate pending call
func (rc *RequestCorrelator) HandleResponse(response *pb.RpcResponse) error {
	call, exists := rc.GetPendingCall(response.RequestId)
	if !exists {
		return status.Errorf(codes.NotFound, "pending call not found: %s", response.RequestId)
	}

	// Check if call context is still valid
	select {
	case <-call.Context.Done():
		rc.CompletePendingCall(response.RequestId, call.Context.Err())
		return call.Context.Err()
	default:
	}

	// Handle different payload types
	switch payload := response.Payload.(type) {
	case *pb.RpcResponse_Response:
		// Single response (unary or client streaming)
		select {
		case call.ResponseChan <- payload.Response:
		case <-call.Context.Done():
			return call.Context.Err()
		}

	case *pb.RpcResponse_StreamMessage:
		// Stream message (server or bidirectional streaming)
		if err := rc.handleStreamMessage(call, payload.StreamMessage); err != nil {
			return err
		}
	}

	// Handle status if present
	if response.Status != nil {
		if response.Status.Code != 0 {
			// Error status
			err := status.Errorf(codes.Code(response.Status.Code), response.Status.Message)
			select {
			case call.ErrorChan <- err:
			case <-call.Context.Done():
			}
			rc.CompletePendingCall(response.RequestId, err)
		} else {
			// Success status
			select {
			case call.StatusChan <- response.Status:
			case <-call.Context.Done():
			}

			// For unary and client streaming, completion means we're done
			if call.MethodType == pb.RpcMethodType_UNARY || call.MethodType == pb.RpcMethodType_CLIENT_STREAMING {
				rc.CompletePendingCall(response.RequestId, nil)
			}
		}
	}

	return nil
}

// handleStreamMessage processes a stream message
func (rc *RequestCorrelator) handleStreamMessage(call *PendingCall, msg *pb.StreamMessage) error {
	switch msg.Control {
	case pb.StreamControl_MESSAGE:
		// Regular stream message
		select {
		case call.StreamChan <- msg:
		case <-call.Context.Done():
			return call.Context.Err()
		}

	case pb.StreamControl_HALF_CLOSE:
		// Server finished sending
		call.ServerStreamDone = true
		close(call.StreamChan)

	case pb.StreamControl_COMPLETE:
		// Stream completed successfully
		call.ServerStreamDone = true
		close(call.StreamChan)
		rc.CompletePendingCall(call.RequestID, nil)

	case pb.StreamControl_ERROR:
		// Stream error
		close(call.StreamChan)
		err := status.Errorf(codes.Unknown, "stream error")
		select {
		case call.ErrorChan <- err:
		case <-call.Context.Done():
		}
		rc.CompletePendingCall(call.RequestID, err)
	}

	return nil
}

// CompletePendingCall removes a pending call and cleans up resources
func (rc *RequestCorrelator) CompletePendingCall(requestID string, err error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	call, exists := rc.pendingCalls[requestID]
	if !exists {
		return
	}

	// Cancel the context
	call.Cancel()

	// Close channels
	close(call.ResponseChan)
	close(call.StatusChan)
	close(call.ErrorChan)
	if !call.ServerStreamDone {
		close(call.StreamChan)
	}

	// Remove from pending calls
	delete(rc.pendingCalls, requestID)
}

// handleTimeout handles request timeouts
func (rc *RequestCorrelator) handleTimeout(call *PendingCall) {
	<-call.Context.Done()

	if call.Context.Err() == context.DeadlineExceeded {
		rc.CompletePendingCall(call.RequestID, context.DeadlineExceeded)
	}
}

// generateRequestID generates a unique request ID
func generateRequestID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := range result {
		result[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(result)
}
