package graft

import (
	"context"
	"sync"
	"sync/atomic"

	"graft/pb"
)

type (
	GraftInstance[T any] struct {
		machineId int64

		electionState electionState
		leaderState   leaderState
		logLock       sync.Mutex
		log           Log[T]

		operationHandler func(T)
		cluster          map[int64]pb.GraftClient

		// grpc implementation
		pb.UnimplementedGraftServer
	}

	// models any meta-data associated with an election
	electionState struct {
		electionStateLock sync.Mutex
		electionTimer     *triggerElectionTimer
		currentTerm       int64
		hasVoted          bool
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		leaderStateLock sync.Mutex
		isLeader        bool
		nextIndex       map[int64]int
		matchIndex      map[int64]int
	}
)

// CreateGraftInstance creates a global graft instance thats ready to be spun up :)
// configPath is the path to the yaml file that contains the configuration for this raft cluster
// operationHandler is the handler invoked when the distributed log has committed an operation
func CreateGraftInstance[T any](operationHandler func(T)) *GraftInstance[T] {
	return &GraftInstance[T]{
		leaderState: leaderState{
			nextIndex:  make(map[int64]int),
			matchIndex: make(map[int64]int),
		},
		operationHandler: operationHandler,
		log:              NewLog[T](),
	}
}

// InitFromConfig initializes the graft instance by parsing the provided config, starting an RPC server and then connecting to
// the RPC servers for the other members of the cluster
func (m *GraftInstance[T]) InitFromConfig(configPath string, machineId int64) {
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

	// construct the election timer and the leadership state
	m.electionState.electionTimer = newElectionTimer(electionTimeoutBound /* electionTriggerCallback: */, m.startElection)
	m.cluster = cluster
	m.machineId = machineId
}

// switchToLeader toggles a machine as the new leader of the cluster
func (m *GraftInstance[T]) switchToLeader() {
	m.leaderState.isLeader = true
	m.electionState.electionTimer.disable()
}

// switchToFollower is the opposite direction and moves the machine
// to a follower state
func (m *GraftInstance[T]) switchToFollower() {
	m.leaderState.isLeader = false
	m.electionState.electionTimer.start()
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
	logHead := m.log.GetHead()

	for _, clusterMember := range m.cluster {
		go func(clusterMember pb.GraftClient) {
			// TODO: not this :D
			ctx := context.TODO()
			voteResult, _ := clusterMember.RequestVote(ctx, &pb.RequestVoteArgs{
				Term:         m.electionState.currentTerm,
				CandidateId:  m.machineId,
				LastLogIndex: int64(len(m.log.entries) - 1),
				LastLogTerm:  int64(logHead.applicationTerm),
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

// === gRPC stub implementations ===
// RequestVote is invoked by a secondary machine in the cluster thats trying to acquire a vote from this machine
func (m *GraftInstance[T]) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	m.electionState.electionStateLock.Lock()
	defer m.electionState.electionStateLock.Unlock()

	m.logLock.Lock()
	defer m.logLock.Unlock()

	logUpToDate := m.log.entries[args.LastLogIndex].applicationTerm == int(args.LastLogTerm)
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
