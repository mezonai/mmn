// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.5.1
// - protoc             v6.31.1
// source: tx.proto

package proto

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
	TxService_TxBroadcast_FullMethodName = "/mmn.TxService/TxBroadcast"
)

// TxServiceClient is the client API for TxService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type TxServiceClient interface {
	TxBroadcast(ctx context.Context, in *TxRequest, opts ...grpc.CallOption) (*TxResponse, error)
}

type txServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewTxServiceClient(cc grpc.ClientConnInterface) TxServiceClient {
	return &txServiceClient{cc}
}

func (c *txServiceClient) TxBroadcast(ctx context.Context, in *TxRequest, opts ...grpc.CallOption) (*TxResponse, error) {
	cOpts := append([]grpc.CallOption{grpc.StaticMethod()}, opts...)
	out := new(TxResponse)
	err := c.cc.Invoke(ctx, TxService_TxBroadcast_FullMethodName, in, out, cOpts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TxServiceServer is the server API for TxService service.
// All implementations must embed UnimplementedTxServiceServer
// for forward compatibility.
type TxServiceServer interface {
	TxBroadcast(context.Context, *TxRequest) (*TxResponse, error)
	mustEmbedUnimplementedTxServiceServer()
}

// UnimplementedTxServiceServer must be embedded to have
// forward compatible implementations.
//
// NOTE: this should be embedded by value instead of pointer to avoid a nil
// pointer dereference when methods are called.
type UnimplementedTxServiceServer struct{}

func (UnimplementedTxServiceServer) TxBroadcast(context.Context, *TxRequest) (*TxResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method TxBroadcast not implemented")
}
func (UnimplementedTxServiceServer) mustEmbedUnimplementedTxServiceServer() {}
func (UnimplementedTxServiceServer) testEmbeddedByValue()                   {}

// UnsafeTxServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to TxServiceServer will
// result in compilation errors.
type UnsafeTxServiceServer interface {
	mustEmbedUnimplementedTxServiceServer()
}

func RegisterTxServiceServer(s grpc.ServiceRegistrar, srv TxServiceServer) {
	// If the following call pancis, it indicates UnimplementedTxServiceServer was
	// embedded by pointer and is nil.  This will cause panics if an
	// unimplemented method is ever invoked, so we test this at initialization
	// time to prevent it from happening at runtime later due to I/O.
	if t, ok := srv.(interface{ testEmbeddedByValue() }); ok {
		t.testEmbeddedByValue()
	}
	s.RegisterService(&TxService_ServiceDesc, srv)
}

func _TxService_TxBroadcast_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(TxRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(TxServiceServer).TxBroadcast(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: TxService_TxBroadcast_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(TxServiceServer).TxBroadcast(ctx, req.(*TxRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// TxService_ServiceDesc is the grpc.ServiceDesc for TxService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var TxService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "mmn.TxService",
	HandlerType: (*TxServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "TxBroadcast",
			Handler:    _TxService_TxBroadcast_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "tx.proto",
}
