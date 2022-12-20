package graft

import (
	"context"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"graft/pb"

	"google.golang.org/grpc"
)

const (
	HEARTBEAT_DURATION = 50 * time.Millisecond
)

type (
	GraftInstance[T any] struct {
		machineId int64

		electionState electionState
		leaderState   leaderState
		logLock       sync.Mutex
		log           Log[T]

		operationHandler func(T)
		serializer       Serializer[T]
		cluster          map[int64]pb.GraftClient

		// grpc implementation
		pb.UnimplementedGraftServer
	}

	// models any meta-data associated with an election
	electionState struct {
		electionStateLock sync.Mutex

		electionTimer *triggerElectionTimer
		currentTerm   int64
		hasVoted      bool
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		leaderStateLock sync.Mutex

		heartbeatTimer *time.Timer
		isLeader       bool
		nextIndex      map[int64]int
		matchIndex     map[int64]int
	}
)

// CreateGraftInstance creates a global graft instance thats ready to be spun up :)
// configPath is the path to the yaml file that contains the configuration for this raft cluster
// operationHandler is the handler invoked when the distributed log has committed an operation
// note: by default each graft instance starts off as a follower
func CreateGraftInstance[T any](operationHandler func(T), serializer Serializer[T]) *GraftInstance[T] {
	return &GraftInstance[T]{
		leaderState: leaderState{
			nextIndex:  make(map[int64]int),
			matchIndex: make(map[int64]int),
		},
		operationHandler: operationHandler,
		log:              NewLog[T](),
	}
}

// StartFromConfig starts the graft instance by parsing the provided config, starting an RPC server and then connecting to
// the RPC servers for the other members of the cluster, it then starts the instance off as a follower
func (m *GraftInstance[T]) StartFromConfig(configPath string, machineId int64) {
	m.startRPCServer()

	electionTimeoutBound, clusterConfig := parseConfiguration(configPath)
	cluster := make(map[int64]pb.GraftClient)

	// dial each other member in the cluster
	for memberId, addr := range clusterConfig {
		if memberId != machineId {
			cluster[memberId] = connectToClusterMember(addr)
			m.leaderState.matchIndex[memberId] = -1
			m.leaderState.nextIndex[memberId] = -1
		}
	}

	// general metadata
	m.cluster = cluster
	m.machineId = machineId

	// construct the election timer and the leadership state
	m.electionState.electionTimer = newElectionTimer(electionTimeoutBound /* electionTriggerCallback: */, m.startElection)
	m.leaderState.heartbeatTimer = time.AfterFunc(HEARTBEAT_DURATION, m.sendHeartbeat)
	m.leaderState.heartbeatTimer.Stop()

	m.switchToFollower()
}

// switchToLeader toggles a machine as the new leader of the cluster
func (m *GraftInstance[T]) switchToLeader() {
	m.leaderState.isLeader = true
	m.electionState.electionTimer.disable()
	m.leaderState.heartbeatTimer.Reset(HEARTBEAT_DURATION)
}

// switchToFollower is the opposite direction and moves the machine
// to a follower state
func (m *GraftInstance[T]) switchToFollower() {
	m.leaderState.isLeader = false
	m.electionState.electionTimer.start()
	m.leaderState.heartbeatTimer.Stop()
}

// startElection starts an election and pushes out requests for votes from all candidates
// the cycle ends if we don't get enough votes :(
func (m *GraftInstance[T]) startElection() {
	m.electionState.electionStateLock.Lock()
	defer m.electionState.electionStateLock.Unlock()

	m.logLock.Lock()
	defer m.logLock.Unlock()

	// start new term and vote for yourself
	m.electionState.currentTerm += 1
	m.electionState.hasVoted = true

	// request a vote from each client now
	accumulatedVotes := int32(1)

	for _, clusterMember := range m.cluster {
		go func(clusterMember pb.GraftClient) {
			// TODO: not this :D
			ctx := context.TODO()
			voteResult, _ := clusterMember.RequestVote(ctx, &pb.RequestVoteArgs{
				Term:         m.electionState.currentTerm,
				CandidateId:  m.machineId,
				LastLogIndex: m.log.HeadIndex(),
				LastLogTerm:  m.log.GetHead().applicationTerm,
			})

			if voteResult.VoteGranted {
				atomic.AddInt32(&accumulatedVotes, 1)
			}
		}(clusterMember)
	}

	wonElection := accumulatedVotes > int32((len(m.cluster)+1)/2)
	if wonElection {
		m.switchToLeader()
	}
}

// sendHeartbeat is the heartbeat function that leaders use to assert dominance over followers
func (m *GraftInstance[T]) sendHeartbeat() {
	for _, clusterMember := range m.cluster {
		go func(clusterMember pb.GraftClient) {
		}(clusterMember)
	}

	m.leaderState.heartbeatTimer.Reset(HEARTBEAT_DURATION)
}

// === gRPC stub implementations ===
// RequestVote is invoked by a secondary machine in the cluster thats trying to acquire a vote from this machine
func (m *GraftInstance[T]) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	m.electionState.electionStateLock.Lock()
	defer m.electionState.electionStateLock.Unlock()

	m.logLock.Lock()
	defer m.logLock.Unlock()

	logUpToDate := m.log.entries[args.LastLogIndex].applicationTerm == args.LastLogTerm
	if args.Term < m.electionState.currentTerm || !logUpToDate || m.electionState.hasVoted {
		return &pb.RequestVoteResponse{
			VoteGranted: false,
			CurrentTerm: m.electionState.currentTerm,
		}, nil
	}

	m.electionState.hasVoted = true
	return &pb.RequestVoteResponse{
		VoteGranted: true,
		CurrentTerm: m.electionState.currentTerm,
	}, nil
}

// AppendEntries is the appendEntries RPC in the original raft paper, the arguments are defined in the proto file
func (m *GraftInstance[T]) AppendEntries(ctx context.Context, args *pb.AppendEntriesArgs) (*pb.AppendEntriesResponse, error) {
	m.electionState.electionStateLock.Lock()
	defer m.electionState.electionStateLock.Unlock()

	m.logLock.Lock()
	defer m.logLock.Unlock()

	if args.Term < m.electionState.currentTerm || m.log.entries[args.PrevLogIndex].applicationTerm != args.PrevLogTerm {
		return &pb.AppendEntriesResponse{
			CurrentTerm: m.electionState.currentTerm,
			Accepted:    false,
		}, nil
	}

	// apply the entries to the log now :)
	parsedEntries := m.parseEntries(args.Entries)
	newEntries := parsedEntries[int(args.PrevLogIndex)-len(m.log.entries)-1:]
	overriddenEntries := parsedEntries[:int(args.PrevLogIndex)-len(m.log.entries)-1]

	// override entries
	for i, v := range overriddenEntries {
		m.log.entries[len(m.log.entries)-i] = v
	}

	// append new entries
	m.log.entries = append(m.log.entries, newEntries...)

	// finally it may be time to start committing entries
	newCommitIndex := int(math.Min(float64(args.LeaderCommit), float64(len(m.log.entries))))
	if args.LeaderCommit > int64(m.log.lastCommitted) {
		for _, logEntry := range m.log.entries[m.log.lastCommitted:newCommitIndex] {
			m.operationHandler(logEntry.operation)
		}

		m.log.lastCommitted = newCommitIndex
	}

	return &pb.AppendEntriesResponse{
		CurrentTerm: m.electionState.currentTerm,
		Accepted:    true,
	}, nil
}

// RPC helper functions
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
	pb.RegisterGraftServer(server, m)

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

func (m *GraftInstance[T]) parseEntries(entries []*pb.LogEntry) []LogEntry[T] {
	parsedEntries := []LogEntry[T]{}
	for _, entry := range entries {
		parsedEntries = append(parsedEntries, LogEntry[T]{
			applicationTerm: entry.ApplicationTerm,
			operation:       m.serializer.FromString(entry.Entry),
		})
	}

	return parsedEntries
}
