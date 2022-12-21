package graft

import (
	"context"
	"net"
	"sync"
	"time"

	"graft/pb"

	"google.golang.org/grpc"
)

const (
	HEARTBEAT_DURATION = 50 * time.Millisecond
)

type (
	GraftInstance[T any] struct {
		machineId     machineID
		electionState electionState
		leaderState   leaderState
		log           Log[T]

		committedOperationCallback func(T)
		cluster                    *cluster

		// grpc implementation
		pb.UnimplementedGraftServer
	}

	// models any meta-data associated with an election
	electionState struct {
		sync.Mutex
		electionTimer *electionTimer
		currentTerm   int64
		hasVoted      bool
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		sync.Mutex
		heartbeatTimer *time.Timer
		nextIndex      map[machineID]int
		matchIndex     map[machineID]int
	}
)

// NewGraftInstance constructs a new graft instance however it starts off as disabled
func NewGraftInstance[T any](configuration []byte, thisMachineID machineID) *GraftInstance[T] {
	graftConfig := parseGraftConfig(configuration)
	cluster := connectToCluster(graftConfig, thisMachineID)
	instance := &GraftInstance[T]{
		machineId: thisMachineID,
		cluster:   cluster,
		log:       NewLog[T](),
		leaderState: leaderState{
			nextIndex:  make(map[machineID]int),
			matchIndex: make(map[machineID]int),
		},
	}

	// setup timers and callbacks
	instance.electionState.electionTimer = newElectionTimer(graftConfig /* electionInvokedCallback: */, instance.runElection)
	instance.leaderState.heartbeatTimer = time.AfterFunc(HEARTBEAT_DURATION, instance.sendHeartbeat)
	instance.leaderState.heartbeatTimer.Stop()

	return instance
}

// start loads up and initializes this graft instance (start it up as a follower and everything)
func (m *GraftInstance[T]) Start() {
	// start the RPC server for this graft instance and switch to follower mode
	// start an RPC server on 8080
	lis, err := net.Listen("tcp", ":8080")
	server := grpc.NewServer()
	pb.RegisterGraftServer(server, m)
	if err != nil {
		panic(err)
	}

	// start the server in a different goroutine from main
	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()

	m.transitionToFollowerMode()
}

// transitions the graft state machine to follower mode
func (m *GraftInstance[T]) transitionToFollowerMode() {
	m.electionState.electionTimer.start()
	m.leaderState.heartbeatTimer.Stop()
	m.cluster.cancelAllOutboundReqs()
}

// transitions the graft state machine to leader mode
func (m *GraftInstance[T]) transitionToLeaderMode() {
	m.electionState.electionTimer.disable()
	m.leaderState.heartbeatTimer.Reset(HEARTBEAT_DURATION)
}

// runElection triggers an election by sending vote requests to all machines within the cluster
func (m *GraftInstance[T]) runElection() {
	m.log.Lock()
	m.electionState.Lock()
	defer m.log.Unlock()
	defer m.electionState.Unlock()

	totalVotes, newTerm := m.cluster.requestVote(&pb.RequestVoteArgs{
		Term:         m.electionState.currentTerm,
		CandidateId:  m.machineId,
		LastLogIndex: m.log.HeadIndex(),
		LastLogTerm:  m.log.GetHead().applicationTerm,
	})

	wonElection := totalVotes > (m.cluster.clusterSize()/2) && newTerm == int(m.electionState.currentTerm)
	if wonElection {
		m.transitionToLeaderMode()
	}

	m.electionState.currentTerm = int64(newTerm)
}

// sendHeartbeat involves just updating every single machine in the cluster with knowledge of the new leader
// unlike the actual operation to push new log entries we do not have to wait for everything to respond
func (m *GraftInstance[T]) sendHeartbeat() {
	m.log.Lock()
	defer m.log.Unlock()

	for memberID := range m.cluster.machines {
		// todo: abstract this out to be a general generic function

		m.electionState.Lock()
		heartbeatArgs := &pb.AppendEntriesArgs{
			Term:         m.electionState.currentTerm,
			LeaderId:     m.machineId,
			PrevLogIndex: m.log.HeadIndex(),
			PrevLogTerm:  m.log.GetHead().applicationTerm,
			LeaderCommit: int64(m.log.lastCommitted),
			Entries:      m.log.SerializeSubset(m.leaderState.nextIndex[memberID]),
		}
		m.electionState.Unlock()

		go func(memberID machineID) {
			// the response from this heartbeat may dictate that we need to move to follower mode
			// ie. we had an outdated term, also worth noting that the switch to follower function will
			// already cancel any outbound requests
			response := m.cluster.pushEntryToClusterMember(memberID, heartbeatArgs)

			if !response.Accepted {
				// request could have failed because of either a log inconsistency or because we're not actually leader
				// deal with each case separately
				m.electionState.Lock()
				switch {
				case response.CurrentTerm > m.electionState.currentTerm:
					m.electionState.currentTerm = response.CurrentTerm
					m.transitionToFollowerMode()
				}
				m.electionState.Unlock()
			}
		}(memberID)
	}
}

// ==== gRPC stub implementations ====
// basically all of these are blind copies from the original raft paper with minimal modification

// RequestVote is the (follower) implementation for the election workflow, it responds when a candidate requests a vote
func (m *GraftInstance[T]) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.log.Lock()
	m.electionState.Lock()
	defer m.electionState.Unlock()
	defer m.log.Unlock()

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

// AppendEntries is the follower side of the log replication component of raft
func (m *GraftInstance[T]) AppendEntries(ctx context.Context, args *pb.AppendEntriesArgs) (*pb.AppendEntriesResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.log.Lock()
	m.electionState.Lock()
	defer m.electionState.Unlock()
	defer m.log.Unlock()

	if args.Term < m.electionState.currentTerm || m.log.entries[args.PrevLogIndex].applicationTerm != args.PrevLogTerm {
		return &pb.AppendEntriesResponse{
			CurrentTerm: m.electionState.currentTerm,
			Accepted:    false,
		}, nil
	}

	// apply the entries and commit anything thats required
	m.log.ApplyEntries(args.Entries, int(args.PrevLogIndex), args.PrevLogTerm)
	newCommitIndex := min(int(args.LeaderCommit), len(m.log.entries))
	for _, op := range m.log.entries[m.log.lastCommitted:newCommitIndex] {
		m.committedOperationCallback(op.operation)
	}

	m.log.lastCommitted = newCommitIndex

	return &pb.AppendEntriesResponse{
		CurrentTerm: m.electionState.currentTerm,
		Accepted:    true,
	}, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
