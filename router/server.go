package router

import (
	"context"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/panyam/grpcrouter/proto/gen/go/grpcrouter/v1"
)

// RouterServer implements the Router service
type RouterServer struct {
	pb.UnimplementedRouterServer

	// Core components
	correlator *RequestCorrelator
	registry   *ServiceRegistry

	// Configuration
	config *RouterConfig
}

// RouterConfig holds router configuration
type RouterConfig struct {
	DefaultTimeout      time.Duration
	MaxConcurrentRPCs   int
	HealthCheckInterval time.Duration
}

// ServiceInstance represents a connected service instance
type ServiceInstance struct {
	InstanceID    string
	ServiceName   string
	Endpoint      string
	Metadata      map[string]string
	HealthStatus  pb.HealthStatus
	RegisteredAt  time.Time
	LastHeartbeat time.Time

	// Streaming connection
	Stream      pb.Router_RegisterServer
	StreamMutex sync.RWMutex

	// Request queues
	RequestQueue  chan *pb.RpcCall
	ResponseQueue chan *pb.RpcResponse

	// State management
	Connected bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewRouterServer creates a new router server
func NewRouterServer(config *RouterConfig) *RouterServer {
	if config == nil {
		config = &RouterConfig{
			DefaultTimeout:      30 * time.Second,
			MaxConcurrentRPCs:   1000,
			HealthCheckInterval: 30 * time.Second,
		}
	}

	return &RouterServer{
		correlator: NewRequestCorrelator(),
		registry:   NewServiceRegistry(),
		config:     config,
	}
}

// Register handles service instance registration and maintains bidirectional streaming
func (rs *RouterServer) Register(stream pb.Router_RegisterServer) error {
	ctx := stream.Context()

	// Wait for initial registration
	req, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive registration: %v", err)
	}

	instanceInfo := req.GetInstanceInfo()
	if instanceInfo == nil {
		return status.Errorf(codes.InvalidArgument, "first message must contain instance info")
	}

	// Create service instance
	instance := &ServiceInstance{
		InstanceID:    instanceInfo.InstanceId,
		ServiceName:   instanceInfo.ServiceName,
		Endpoint:      instanceInfo.Endpoint,
		Metadata:      instanceInfo.Metadata,
		HealthStatus:  instanceInfo.HealthStatus,
		RegisteredAt:  time.Now(),
		LastHeartbeat: time.Now(),
		Stream:        stream,
		RequestQueue:  make(chan *pb.RpcCall, 100),
		ResponseQueue: make(chan *pb.RpcResponse, 100),
		Connected:     true,
	}

	instance.ctx, instance.cancel = context.WithCancel(ctx)

	// Register the instance
	if err := rs.registry.RegisterInstance(instance); err != nil {
		return status.Errorf(codes.AlreadyExists, "failed to register instance: %v", err)
	}

	// Send registration acknowledgment
	ack := &pb.RegisterResponse{
		Response: &pb.RegisterResponse_Ack{
			Ack: &pb.RegistrationAck{
				InstanceId: instanceInfo.InstanceId,
				Success:    true,
				Message:    "Registration successful",
			},
		},
	}

	if err := stream.Send(ack); err != nil {
		rs.registry.UnregisterInstance(instanceInfo.InstanceId)
		return status.Errorf(codes.Internal, "failed to send acknowledgment: %v", err)
	}

	log.Printf("Service instance registered: %s (%s)", instanceInfo.InstanceId, instanceInfo.ServiceName)

	// Start goroutines for handling the instance
	go rs.handleInstanceRequests(instance)
	go rs.handleInstanceResponses(instance)

	// Handle incoming messages from the instance
	return rs.handleInstanceMessages(instance)
}

// handleInstanceMessages processes incoming messages from a service instance
func (rs *RouterServer) handleInstanceMessages(instance *ServiceInstance) error {
	defer func() {
		rs.registry.UnregisterInstance(instance.InstanceID)
		instance.cancel()
		log.Printf("Service instance disconnected: %s", instance.InstanceID)
	}()

	for {
		req, err := instance.Stream.Recv()
		if err != nil {
			return err
		}

		switch msg := req.Request.(type) {
		case *pb.RegisterRequest_Heartbeat:
			if err := rs.handleHeartbeat(instance, msg.Heartbeat); err != nil {
				return err
			}

		case *pb.RegisterRequest_RpcResponse:
			if err := rs.handleRpcResponse(instance, msg.RpcResponse); err != nil {
				log.Printf("Error handling RPC response: %v", err)
			}

		case *pb.RegisterRequest_Unregister:
			log.Printf("Instance %s requested unregistration: %s", instance.InstanceID, msg.Unregister.Reason)
			return nil

		default:
			log.Printf("Unknown message type from instance %s", instance.InstanceID)
		}
	}
}

// handleInstanceRequests sends RPC calls to the service instance
func (rs *RouterServer) handleInstanceRequests(instance *ServiceInstance) {
	for {
		select {
		case <-instance.ctx.Done():
			return

		case rpcCall := <-instance.RequestQueue:
			response := &pb.RegisterResponse{
				Response: &pb.RegisterResponse_RpcCall{
					RpcCall: rpcCall,
				},
			}

			instance.StreamMutex.RLock()
			err := instance.Stream.Send(response)
			instance.StreamMutex.RUnlock()

			if err != nil {
				log.Printf("Failed to send RPC call to instance %s: %v", instance.InstanceID, err)
				// TODO: Handle failed request - perhaps retry or mark instance as unhealthy
			}
		}
	}
}

// handleInstanceResponses processes RPC responses from the service instance
func (rs *RouterServer) handleInstanceResponses(instance *ServiceInstance) {
	for {
		select {
		case <-instance.ctx.Done():
			return

		case rpcResponse := <-instance.ResponseQueue:
			if err := rs.correlator.HandleResponse(rpcResponse); err != nil {
				log.Printf("Failed to handle RPC response: %v", err)
			}
		}
	}
}

// handleHeartbeat processes heartbeat messages from service instances
func (rs *RouterServer) handleHeartbeat(instance *ServiceInstance, heartbeat *pb.Heartbeat) error {
	instance.LastHeartbeat = time.Now()
	instance.HealthStatus = heartbeat.HealthStatus

	// Update registry
	rs.registry.UpdateInstanceHealth(instance.InstanceID, heartbeat.HealthStatus)

	return nil
}

// handleRpcResponse processes RPC responses from service instances
func (rs *RouterServer) handleRpcResponse(instance *ServiceInstance, response *pb.RpcResponse) error {
	// Queue the response for processing
	select {
	case instance.ResponseQueue <- response:
		return nil
	case <-instance.ctx.Done():
		return instance.ctx.Err()
	default:
		return status.Errorf(codes.ResourceExhausted, "response queue full for instance %s", instance.InstanceID)
	}
}

// HealthCheck implements the HealthCheck RPC
func (rs *RouterServer) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	instance, err := rs.registry.GetInstance(req.InstanceId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "instance not found: %s", req.InstanceId)
	}

	return &pb.HealthCheckResponse{
		InstanceId: req.InstanceId,
		Status:     instance.HealthStatus,
		Message:    "Health check successful",
	}, nil
}

// ListInstances implements the ListInstances RPC
func (rs *RouterServer) ListInstances(ctx context.Context, req *pb.ListInstancesRequest) (*pb.ListInstancesResponse, error) {
	instances := rs.registry.ListInstances(req.ServiceName, req.StatusFilter)

	var instanceInfos []*pb.InstanceInfo
	for _, instance := range instances {
		instanceInfos = append(instanceInfos, &pb.InstanceInfo{
			InstanceId:   instance.InstanceID,
			ServiceName:  instance.ServiceName,
			Endpoint:     instance.Endpoint,
			Metadata:     instance.Metadata,
			HealthStatus: instance.HealthStatus,
			RegisteredAt: timestamppb.New(instance.RegisteredAt),
		})
	}

	return &pb.ListInstancesResponse{
		Instances: instanceInfos,
	}, nil
}

// RouteRPC routes an RPC call to an appropriate service instance
func (rs *RouterServer) RouteRPC(
	ctx context.Context,
	serviceName string,
	method string,
	methodType pb.RpcMethodType,
	request *anypb.Any,
	metadata map[string]string,
	routingKey string,
) (*PendingCall, error) {
	// Find appropriate instance
	instance, err := rs.registry.SelectInstance(serviceName, routingKey)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "no available instance for service %s: %v", serviceName, err)
	}

	// Create pending call
	call := rs.correlator.CreatePendingCall(ctx, method, methodType, rs.config.DefaultTimeout)

	// Create RPC call message
	rpcCall := &pb.RpcCall{
		RequestId:  call.RequestID,
		Method:     method,
		MethodType: methodType,
		Metadata:   metadata,
		Timeout:    timestamppb.New(time.Now().Add(rs.config.DefaultTimeout)),
	}

	// Set payload based on method type
	if methodType == pb.RpcMethodType_UNARY || methodType == pb.RpcMethodType_SERVER_STREAMING {
		rpcCall.Payload = &pb.RpcCall_Request{Request: request}
	} else {
		// For streaming methods, we'll send the initial message
		rpcCall.Payload = &pb.RpcCall_StreamMessage{
			StreamMessage: &pb.StreamMessage{
				Sequence: 1,
				Message:  request,
				Control:  pb.StreamControl_MESSAGE,
			},
		}
	}

	// Send RPC call to instance
	select {
	case instance.RequestQueue <- rpcCall:
		return call, nil
	case <-ctx.Done():
		rs.correlator.CompletePendingCall(call.RequestID, ctx.Err())
		return nil, ctx.Err()
	default:
		rs.correlator.CompletePendingCall(call.RequestID,
			status.Errorf(codes.ResourceExhausted, "request queue full for instance %s", instance.InstanceID))
		return nil, status.Errorf(codes.ResourceExhausted, "request queue full for instance %s", instance.InstanceID)
	}
}

// SendStreamMessage sends a stream message for an active RPC call
func (rs *RouterServer) SendStreamMessage(requestID string, message *anypb.Any, control pb.StreamControl) error {
	call, exists := rs.correlator.GetPendingCall(requestID)
	if !exists {
		return status.Errorf(codes.NotFound, "pending call not found: %s", requestID)
	}

	// Find the instance handling this call
	instance, err := rs.registry.SelectInstance("", "") // TODO: Need to track which instance is handling which call
	if err != nil {
		return err
	}

	streamMessage := &pb.StreamMessage{
		Sequence: call.StreamSequence,
		Message:  message,
		Control:  control,
	}

	call.StreamSequence++

	rpcCall := &pb.RpcCall{
		RequestId:  requestID,
		Method:     call.Method,
		MethodType: call.MethodType,
		Payload: &pb.RpcCall_StreamMessage{
			StreamMessage: streamMessage,
		},
	}

	select {
	case instance.RequestQueue <- rpcCall:
		return nil
	case <-call.Context.Done():
		return call.Context.Err()
	default:
		return status.Errorf(codes.ResourceExhausted, "request queue full")
	}
}

// CleanupStaleInstances removes stale instances
func (rs *RouterServer) CleanupStaleInstances(timeout time.Duration) {
	rs.registry.CleanupStaleInstances(timeout)
}
