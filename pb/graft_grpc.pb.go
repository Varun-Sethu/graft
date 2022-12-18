// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v3.6.1
// source: graft.proto

package pb

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// GraftClient is the client API for Graft service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type GraftClient interface {
	RequestVote(ctx context.Context, in *RequestVoteArgs, opts ...grpc.CallOption) (*RequestVoteResponse, error)
}

type graftClient struct {
	cc grpc.ClientConnInterface
}

func NewGraftClient(cc grpc.ClientConnInterface) GraftClient {
	return &graftClient{cc}
}

func (c *graftClient) RequestVote(ctx context.Context, in *RequestVoteArgs, opts ...grpc.CallOption) (*RequestVoteResponse, error) {
	out := new(RequestVoteResponse)
	err := c.cc.Invoke(ctx, "/Graft/RequestVote", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GraftServer is the server API for Graft service.
// All implementations must embed UnimplementedGraftServer
// for forward compatibility
type GraftServer interface {
	RequestVote(context.Context, *RequestVoteArgs) (*RequestVoteResponse, error)
	mustEmbedUnimplementedGraftServer()
}

// UnimplementedGraftServer must be embedded to have forward compatible implementations.
type UnimplementedGraftServer struct {
}

func (UnimplementedGraftServer) RequestVote(context.Context, *RequestVoteArgs) (*RequestVoteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RequestVote not implemented")
}
func (UnimplementedGraftServer) mustEmbedUnimplementedGraftServer() {}

// UnsafeGraftServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to GraftServer will
// result in compilation errors.
type UnsafeGraftServer interface {
	mustEmbedUnimplementedGraftServer()
}

func RegisterGraftServer(s grpc.ServiceRegistrar, srv GraftServer) {
	s.RegisterService(&Graft_ServiceDesc, srv)
}

func _Graft_RequestVote_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(RequestVoteArgs)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(GraftServer).RequestVote(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/Graft/RequestVote",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(GraftServer).RequestVote(ctx, req.(*RequestVoteArgs))
	}
	return interceptor(ctx, in, info, handler)
}

// Graft_ServiceDesc is the grpc.ServiceDesc for Graft service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var Graft_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "Graft",
	HandlerType: (*GraftServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "RequestVote",
			Handler:    _Graft_RequestVote_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "graft.proto",
}