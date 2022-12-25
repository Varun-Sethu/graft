package graft

import (
	"context"
	"net"
	"testing"

	"graft/pb"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
)

type (
	mockRPCServer struct {
		voteHandler          func(context.Context, *pb.RequestVoteArgs) *pb.RequestVoteResponse
		appendEntriesHandler func(context.Context, *pb.AppendEntriesArgs) *pb.AppendEntriesResponse
		addLogEntryHandler   func(context.Context, *pb.AddLogEntryArgs)

		pb.UnimplementedGraftServer
	}
)

// gRPC stubs for mocking
func (m *mockRPCServer) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	return m.voteHandler(ctx, args), nil
}

func (m *mockRPCServer) AppendEntries(ctx context.Context, args *pb.AppendEntriesArgs) (*pb.AppendEntriesResponse, error) {
	return m.appendEntriesHandler(ctx, args), nil
}

func (m *mockRPCServer) AddLogEntry(ctx context.Context, args *pb.AddLogEntryArgs) (*pb.Empty, error) {
	m.addLogEntryHandler(ctx, args)
	return &pb.Empty{}, nil
}

// starts an instance of the mock server for usage
func (m *mockRPCServer) start(lis net.Listener) {
	server := grpc.NewServer()
	pb.RegisterGraftServer(server, m)

	// start the server in a different goroutine from main
	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()
}

// creates a cluster of machines, returns a raft configuration for this cluster
func createTestCluster(numMachines int) (graftConfig, []*mockRPCServer) {
	machines := []*mockRPCServer{}
	clusterConfig := make(map[machineID]string)

	for machineId := 0; machineId < numMachines; machineId++ {
		listener, _ := net.Listen("tcp", ":0")
		addr := listener.Addr().String()

		machine := &mockRPCServer{}
		machine.start(listener)

		machines = append(machines, machine)
		clusterConfig[int64(machineId)] = addr
	}

	return graftConfig{
		clusterConfig: clusterConfig,
	}, machines
}

func TestRequestVote(t *testing.T) {
	// setup the test
	config, machines := createTestCluster(5)
	cluster := connectToCluster(config /* thisMachinesID = */, 0)

	// setup so we can test that votes are aggregated correctly
	// note: we register a vote handler for this machine too but we should
	// never actually request a vote from it
	for i := 0; i < 5; i++ {
		successfulVote := i != 2
		currentTerm := 3
		if i == 1 || i == 2 {
			currentTerm = 2
		}

		machines[i].voteHandler = func(ctx context.Context, rva *pb.RequestVoteArgs) *pb.RequestVoteResponse {
			return &pb.RequestVoteResponse{
				VoteGranted: successfulVote,
				CurrentTerm: int64(currentTerm),
			}
		}
	}

	assert := assert.New(t)

	numVotes, currentTerm := cluster.requestVote(&pb.RequestVoteArgs{})
	// we should expect 3 votes from the cluster and the current term should be 3
	assert.Equal(3, numVotes)
	assert.Equal(3, currentTerm)

	// we should also cancel votes and default to 0 if they're taking too long
	for i := 1; i < 5; i++ {
		machines[i].voteHandler = func(ctx context.Context, rva *pb.RequestVoteArgs) *pb.RequestVoteResponse {
			select {}
			return &pb.RequestVoteResponse{
				VoteGranted: true,
				CurrentTerm: 1,
			}
		}
	}

	numVotes, currentTerm = cluster.requestVote(&pb.RequestVoteArgs{})
	assert.Equal(0, numVotes)
	assert.Equal(-1, currentTerm)
}

func TestAppendEntry(t *testing.T) {
	config, machines := createTestCluster(5)
	cluster := connectToCluster(config /* thisMachinesID = */, 0)

	// create a handler for for a specific machine
	machines[1].appendEntriesHandler = func(ctx context.Context, aea *pb.AppendEntriesArgs) *pb.AppendEntriesResponse {
		return &pb.AppendEntriesResponse{
			Accepted:    true,
			CurrentTerm: 2,
		}
	}

	assert := assert.New(t)

	// test normal operation
	response := cluster.appendEntryForMember(1, &pb.AppendEntriesArgs{}, 1)
	assert.True(response.Accepted)
	assert.Equal(int64(2), response.CurrentTerm)

	// test requests should timeout
	machines[2].appendEntriesHandler = func(ctx context.Context, aea *pb.AppendEntriesArgs) *pb.AppendEntriesResponse {
		// go to sleep
		select {}
		return &pb.AppendEntriesResponse{
			Accepted:    true,
			CurrentTerm: 2,
		}
	}

	assert.Nil(cluster.appendEntryForMember(2, &pb.AppendEntriesArgs{}, 1))

	// should be able to resend requests after timed out requests
	badHandler := func(ctx context.Context, aea *pb.AppendEntriesArgs) *pb.AppendEntriesResponse {
		// go to sleep
		select {}
		return &pb.AppendEntriesResponse{
			Accepted:    true,
			CurrentTerm: 2,
		}
	}

	goodHandler := func(ctx context.Context, aea *pb.AppendEntriesArgs) *pb.AppendEntriesResponse {
		return &pb.AppendEntriesResponse{
			Accepted:    true,
			CurrentTerm: 2,
		}
	}

	// first make a request with the badHandler and then the goodHandler
	// this test is technica
	machines[3].appendEntriesHandler = badHandler
	assert.Nil(cluster.appendEntryForMember(3, &pb.AppendEntriesArgs{}, 1))

	machines[3].appendEntriesHandler = goodHandler
	resp := cluster.appendEntryForMember(3, &pb.AppendEntriesArgs{}, 1)
	assert.True(resp.Accepted)
	assert.Equal(int64(2), resp.CurrentTerm)
}

func TestPushOperationToServer(t *testing.T) {
	config, machines := createTestCluster(5)
	cluster := connectToCluster(config /* thisMachinesID = */, 0)

	cluster.currentLeader = 1

	// override the entry for just the elected leader
	invoked := make(chan bool)
	machines[1].addLogEntryHandler = func(ctx context.Context, alea *pb.AddLogEntryArgs) {
		invoked <- true
	}

	cluster.pushOperationToLeader("amongus")
	// this test wont ever actually terminate if the operation isnt pushed to the leader
	wasInvoked := <-invoked
	assert.True(t, wasInvoked)
}
