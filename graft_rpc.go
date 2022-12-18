package graft

import (
	"net"

	"graft/pb"

	"google.golang.org/grpc"
)

// small interface for communicating with the other machines in the cluster
// the assumption here is that all the other machines have spun up RPC servers and are capable of starting requests
// hence the initialization method should only be called AFTER starting an RPC server
func (m *GraftInstance[T]) startRPCServer() {
	// start an RPC server on 8080
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	// register this as a server and start it in a new goroutine
	server := grpc.NewServer()
	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()
}

// connectToClusterMember attempts to dial a cluster member and establish an rpc connection
// with them
func connectToClusterMember(addr string) pb.GraftClient {
	opts := []grpc.DialOption{}

	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		panic(err)
	}

	// TODO: close the conn eventually
	return pb.NewGraftClient(conn)
}
