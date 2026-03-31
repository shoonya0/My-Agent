// Package authpb provides hand-registered gRPC service descriptors and clients
// for auth.v1.AuthService using JSON payloads (myAgent/pkg/grpcutil) and
// domain types from myAgent/pkg/model, without protoc-generated stubs.
package authpb

import (
	"context"

	_ "myAgent/pkg/grpcutil" // register JSON codec in init()
	"myAgent/pkg/model"

	"google.golang.org/grpc"
)

// ServiceName is the fully-qualified gRPC service name for AuthService.
const ServiceName = "auth.v1.AuthService"

// AuthServiceServer is the server API for AuthService.
type AuthServiceServer interface {
	ValidateToken(ctx context.Context, req *model.ValidateTokenRequest) (*model.Claims, error)
	Register(ctx context.Context, req *model.RegisterUserRequest) (*model.TokenResponse, error)
}

// RegisterAuthServiceServer registers the implementation srv with the given
// grpc.ServiceRegistrar (e.g. *grpc.Server).
func RegisterAuthServiceServer(s grpc.ServiceRegistrar, srv AuthServiceServer) {
	s.RegisterService(&AuthService_ServiceDesc, srv)
}

// AuthService_ServiceDesc is the grpc.ServiceDesc for AuthService. It is valid
// to pass to grpc.ServiceRegistrar.RegisterService.
var AuthService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: ServiceName,
	HandlerType: (*AuthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "ValidateToken",
			Handler:    _AuthService_ValidateToken_Handler,
		},
		{
			MethodName: "Register",
			Handler:    _AuthService_Register_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "auth.proto",
}

func _AuthService_ValidateToken_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(model.ValidateTokenRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).ValidateToken(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/" + ServiceName + "/ValidateToken",
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(AuthServiceServer).ValidateToken(ctx, req.(*model.ValidateTokenRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AuthService_Register_Handler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(model.RegisterUserRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).Register(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/" + ServiceName + "/Register",
	}
	handler := func(ctx context.Context, req any) (any, error) {
		return srv.(AuthServiceServer).Register(ctx, req.(*model.RegisterUserRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// AuthServiceClient is the client API for AuthService.
type AuthServiceClient interface {
	ValidateToken(ctx context.Context, in *model.ValidateTokenRequest, opts ...grpc.CallOption) (*model.Claims, error)
	Register(ctx context.Context, in *model.RegisterUserRequest, opts ...grpc.CallOption) (*model.TokenResponse, error)
}

type authServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewAuthServiceClient returns a client for AuthService. Callers should use
// a connection configured to negotiate the JSON codec (e.g. via
// grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json"))); each RPC
// also prepends that call option for ValidateToken.
func NewAuthServiceClient(cc grpc.ClientConnInterface) AuthServiceClient {
	return &authServiceClient{cc}
}

func (c *authServiceClient) ValidateToken(ctx context.Context, in *model.ValidateTokenRequest, opts ...grpc.CallOption) (*model.Claims, error) {
	out := new(model.Claims)
	opts = append([]grpc.CallOption{grpc.CallContentSubtype("json")}, opts...)
	err := c.cc.Invoke(ctx, "/"+ServiceName+"/ValidateToken", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *authServiceClient) Register(ctx context.Context, in *model.RegisterUserRequest, opts ...grpc.CallOption) (*model.TokenResponse, error) {
	out := new(model.TokenResponse)
	opts = append([]grpc.CallOption{grpc.CallContentSubtype("json")}, opts...)
	err := c.cc.Invoke(ctx, "/"+ServiceName+"/Register", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}
