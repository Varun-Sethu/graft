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
		sync.Mutex
		pb.UnimplementedGraftServer

		machineId machineID
		cluster   *cluster

		electionState electionState
		leaderState   leaderState
		log           Log[T]
	}

	// models any meta-data associated with an election
	electionState struct {
		electionTimer *electionTimer
		currentTerm   int64
		hasVoted      bool
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		heartbeatTimer *time.Timer
		nextIndex      map[machineID]int
		matchIndex     map[machineID]int
	}
)

// NewGraftInstance constructs a new graft instance however it starts off as disabled
func NewGraftInstance[T any](configuration []byte, thisMachineID machineID, operationCommitCallback func(T)) *GraftInstance[T] {
	graftConfig := parseGraftConfig(configuration)
	cluster := connectToCluster(graftConfig, thisMachineID)
	instance := &GraftInstance[T]{
		machineId: thisMachineID,
		cluster:   cluster,
		log:       newLog(operationCommitCallback),
		leaderState: leaderState{
			nextIndex:  make(map[machineID]int),
			matchIndex: make(map[machineID]int),
		},
	}

	// setup timers and callbacks
	instance.electionState.electionTimer = newElectionTimer(graftConfig /* electionRunner: */, instance.runElection)
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

	m.transitionToFollowerMode( /* term = */ 0)
}

// transitions the graft state machine to follower mode
func (m *GraftInstance[T]) transitionToFollowerMode(newTerm int64) {
	m.electionState.currentTerm = newTerm
	m.electionState.hasVoted = false

	m.electionState.electionTimer.start()
	m.leaderState.heartbeatTimer.Stop()
}

// transitions the graft state machine to leader mode
func (m *GraftInstance[T]) transitionToLeaderMode() {
	m.leaderState.heartbeatTimer.Reset(HEARTBEAT_DURATION)
}

// runElection triggers an election by sending vote requests to all machines within the cluster
func (m *GraftInstance[T]) runElection() bool {
	m.Lock()
	defer m.Unlock()

	totalVotes, newTerm := m.cluster.requestVote(&pb.RequestVoteArgs{
		Term:         m.electionState.currentTerm,
		CandidateId:  m.machineId,
		LastLogIndex: m.log.lastIndex(),
		LastLogTerm:  m.log.getLastEntry().applicationTerm,
	})

	m.electionState.currentTerm = int64(newTerm)
	hasWonElection := totalVotes > (m.cluster.clusterSize()/2) && newTerm == int(m.electionState.currentTerm)
	if hasWonElection {
		m.transitionToLeaderMode()
	}

	return hasWonElection
}

// sendHeartbeat involves just updating every single machine in the cluster with knowledge of the new leader
// unlike the actual operation to push new log entries we do not have to wait for everything to respond
func (m *GraftInstance[T]) sendHeartbeat() {
	m.Lock()
	currentTerm := m.electionState.currentTerm
	m.Unlock()

	for memberID := range m.cluster.machines {
		go func(memberID machineID) {
			m.Lock()
			heartbeatArgs := &pb.AppendEntriesArgs{
				Term:         currentTerm,
				LeaderId:     m.machineId,
				PrevLogIndex: m.log.lastIndex(),
				PrevLogTerm:  m.log.getLastEntry().applicationTerm,
				LeaderCommit: int64(m.log.lastCommitted),
				Entries: m.log.serializeSubset(
					/*rangeStart = */ m.leaderState.matchIndex[memberID],
					/* rangeEnd = */ m.leaderState.nextIndex[memberID],
				),
			}
			m.Unlock()
			// the response from this heartbeat may dictate that we need to move to follower mode
			// ie. we had an outdated term, also worth noting that the switch to follower function will
			// already cancel any outbound requests
			response := m.cluster.appendEntryForMember(memberID, heartbeatArgs, currentTerm)

			m.Lock()
			if response.Accepted {
				// update the meta-data tracking how much of memberID's log matches with outs
				m.leaderState.matchIndex[memberID] = m.leaderState.nextIndex[memberID]
				m.leaderState.nextIndex[memberID] += 1

				// accepted response so its worth checking if we can update the commit index
				// basically we want to see how much we can increase the commit index by
				nextCommittableOperation := m.log.lastCommitted
				for operationIndex := m.log.lastCommitted + 1; operationIndex < len(m.log.entries); operationIndex++ {
					numReplicatedMachines := 0
					for machineId := range m.cluster.machines {
						if m.leaderState.matchIndex[machineId] >= operationIndex {
							numReplicatedMachines += 1
						}
					}

					committable := numReplicatedMachines > (m.cluster.clusterSize() / 2)
					if committable {
						nextCommittableOperation += 1
					} else {
						break
					}
				}

				// trigger a commit for this new committable operation
				m.log.lastCommitted = nextCommittableOperation
			} else {
				// request could have failed because of either a log inconsistency or because we're not actually leader
				// deal with each case separately
				if response.CurrentTerm > m.electionState.currentTerm {
					m.transitionToFollowerMode( /* newTerm = */ response.CurrentTerm)
				} else {
					// decrement the next index so on the next heartbeat we send more updated values
					m.leaderState.nextIndex[memberID] -= 1
				}
			}

			m.Unlock()
		}(memberID)
	}
}

// ==== gRPC stub implementations ====
// basically all of these are blind copies from the original raft paper with minimal modification

// RequestVote is the (follower) implementation for the election workflow, it responds when a candidate requests a vote
func (m *GraftInstance[T]) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	if contextCancelled, err := contextIsCancelled(ctx); contextCancelled {
		return nil, err
	}

	m.Lock()
	defer m.Unlock()

	// transition to follower mode if we witness our term is outdated
	if args.Term > m.electionState.currentTerm {
		m.transitionToFollowerMode( /* newTerm = */ args.Term)
	}

	logUpToDate := m.log.entries[args.LastLogIndex].applicationTerm == args.LastLogTerm
	eligibleForElection := args.Term >= m.electionState.currentTerm && logUpToDate && !m.electionState.hasVoted

	if eligibleForElection {
		m.electionState.hasVoted = true
	}

	return &pb.RequestVoteResponse{
		VoteGranted: eligibleForElection,
		CurrentTerm: m.electionState.currentTerm,
	}, nil
}

// AppendEntries is the follower side of the log replication component of raft
func (m *GraftInstance[T]) AppendEntries(ctx context.Context, args *pb.AppendEntriesArgs) (*pb.AppendEntriesResponse, error) {
	if contextCancelled, err := contextIsCancelled(ctx); contextCancelled {
		return nil, err
	}

	m.Lock()
	defer m.Unlock()

	// transition to follower mode if we witness our term is outdated
	if args.Term > m.electionState.currentTerm {
		m.transitionToFollowerMode( /* newTerm = */ m.electionState.currentTerm)
	}

	requestIsAccepted := args.Term >= m.electionState.currentTerm && m.log.entries[args.PrevLogIndex].applicationTerm == args.PrevLogTerm
	if requestIsAccepted {
		m.log.appendEntries(args.Entries, int(args.PrevLogIndex), args.PrevLogTerm)
		m.log.updateCommitIndex(int(args.LeaderCommit))
	}

	return &pb.AppendEntriesResponse{
		CurrentTerm: m.electionState.currentTerm,
		Accepted:    requestIsAccepted,
	}, nil
}

// contextIsCancelled is a smaller helper function to determine if a context is cancelled
// primarily used to prevent the unnecessary processing of gRPC calls
func contextIsCancelled(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	default:
		return false, nil
	}
}
