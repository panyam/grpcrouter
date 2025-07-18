// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             (unknown)
// source: grpcrouter/v1/router.proto

package grpcrouterv1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.64.0 or later.
const _ = grpc.SupportPackageIsVersion9

const (
	Router_Register_FullMethodName      = "/grpcrouter.v1.Router/Register"
	Router_HealthCheck_FullMethodName   = "/grpcrouter.v1.Router/HealthCheck"
	Router_ListInstances_FullMethodName = "/grpcrouter.v1.Router/ListInstances"
)

// RouterClient is the client API for Router service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
//
// Router service that manages service instances and routes requests
type RouterClient interface {
	// Register a service instance with the router
	Register(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[RegisterRequest, RegisterResponse], error)
	// Health check for registered instances
	HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
	// List registered instances
	ListInstances(ctx context.Context, in *ListInstancesRequest, opts ...grpc.CallOption) (*ListInstancesResponse, error)
}

type routerClient struct {
	cc grpc.ClientConnInterface
}

func NewRouterClient(cc grpc.ClientConnInterface) RouterClient {
	return &routerClient{cc}
}

func (c *routerClient) Register(ctx context.Context, opts ...grpc.CallOption) (grpc.BidiStreamingClient[RegisterRequest, RegisterResponse], error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	stream, err := c.cc.NewStream(ctx, &Router_ServiceDesc.Streams[0], Router_Register_FullMethodName, cOpts...)
	if err != nil {
		return nil, err
	}
	x := &grpc.GenericClientStream[RegisterRequest, RegisterResponse]{ClientStream: stream}
	return x, nil
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Router_RegisterClient = grpc.BidiStreamingClient[RegisterRequest, RegisterResponse]

func (c *routerClient) HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(HealthCheckResponse)
	err := c.cc.Invoke(ctx, Router_HealthCheck_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *routerClient) ListInstances(ctx context.Context, in *ListInstancesRequest, opts ...grpc.CallOption) (*ListInstancesResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(ListInstancesResponse)
	err := c.cc.Invoke(ctx, Router_ListInstances_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RouterServer is the server API for Router service.
// All implementations must embed UnimplementedRouterServer
// for forward compatibility.
//
// Router service that manages service instances and routes requests
type RouterServer interface {
	// Register a service instance with the router
	Register(grpc.BidiStreamingServer[RegisterRequest, RegisterResponse]) error
	// Health check for registered instances
	HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
	// List registered instances
	ListInstances(context.Context, *ListInstancesRequest) (*ListInstancesResponse, error)
	mustEmbedUnimplementedRouterServer()
}

// UnimplementedRouterServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedRouterServer struct{}

func (UnimplementedRouterServer) Register(grpc.BidiStreamingServer[RegisterRequest, RegisterResponse]) error {
	return status.Errorf(codes.Unimplemented, "method Register not implemented")
}
func (UnimplementedRouterServer) HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method HealthCheck not implemented")
}
func (UnimplementedRouterServer) ListInstances(context.Context, *ListInstancesRequest) (*ListInstancesResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method ListInstances not implemented")
}
func (UnimplementedRouterServer) mustEmbedUnimplementedRouterServer() {}
func (UnimplementedRouterServer) testEmbeddedByValue()                {}

// UnsafeRouterServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to RouterServer will
// result in compilation errors.
type UnsafeRouterServer interface {
	mustEmbedUnimplementedRouterServer()
}

func RegisterRouterServer(s grpc.ServiceRegistrar, srv RouterServer) {
	// If the following call pancis, it indicates UnimplementedRouterServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&Router_ServiceDesc, srv)
}

func _Router_Register_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(RouterServer).Register(&grpc.GenericServerStream[RegisterRequest, RegisterResponse]{ServerStream: stream})
}

// This type alias is provided for backwards compatibility with existing code that references the prior non-generic stream type by name.
type Router_RegisterServer = grpc.BidiStreamingServer[RegisterRequest, RegisterResponse]

func _Router_HealthCheck_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RouterServer).HealthCheck(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Router_HealthCheck_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RouterServer).HealthCheck(ctx, req.(*HealthCheckRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _Router_ListInstances_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListInstancesRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RouterServer).ListInstances(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: Router_ListInstances_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RouterServer).ListInstances(ctx, req.(*ListInstancesRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// Router_ServiceDesc is the grpc.ServiceDesc for Router service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Router_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "grpcrouter.v1.Router",
	HandlerType: (*RouterServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "HealthCheck",
			Handler:    _Router_HealthCheck_Handler,
		},
		{
			MethodName: "ListInstances",
			Handler:    _Router_ListInstances_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Register",
			Handler:       _Router_Register_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "grpcrouter/v1/router.proto",
}
