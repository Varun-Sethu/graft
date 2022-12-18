package graft

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"graft/pb"

	"gopkg.in/yaml.v3"
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
		electionTimer     *electionTimer
		currentTerm       int64
		hasVoted          bool
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		isLeader   bool
		nextIndex  map[int64]int
		matchIndex map[int64]int
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
	m.electionState.electionTimer = newElectionTimer(electionTimeoutBound /* onTimeout: */, m.triggerElection)
	m.cluster = cluster
	m.machineId = machineId
}

// switchToLeader toggles a machine as the new leader of the cluster
// switchToFollower is the opposite direction
func (m *GraftInstance[T]) switchToLeader()   { m.leaderState.isLeader = true }
func (m *GraftInstance[T]) switchToFollower() { m.leaderState.isLeader = false }

// triggerElection starts an election and pushes out requests for votes from all candidates
// the cycle ends if we don't get enough votes :(
func (m *GraftInstance[T]) triggerElection() {
	m.electionState.electionStateLock.Lock()
	defer m.electionState.electionStateLock.Unlock()

	m.logLock.Lock()
	defer m.logLock.Unlock()

	// start new term and vote for yourself
	m.electionState.electionTimer.disable()
	m.electionState.currentTerm += 1
	m.electionState.hasVoted = true

	// request a vote from each client now
	accumulatedVotes := int32(1)
	logHead := m.log.GetHead()

	for _, clusterMember := range m.cluster {
		go func() {
			// TODO: not this :D, dont allow context leaks
			ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
			voteResult, _ := clusterMember.RequestVote(ctx, &pb.RequestVoteArgs{
				Term:         m.electionState.currentTerm,
				CandidateId:  m.machineId,
				LastLogIndex: int64(len(m.log.entries) - 1),
				LastLogTerm:  int64(logHead.applicationTerm),
			})

			if voteResult.VoteGranted {
				atomic.AddInt32(&accumulatedVotes, 1)
			}
		}()
	}

	wonElection := accumulatedVotes > int32((len(m.cluster)+1)/2)
	if wonElection {
		m.switchToLeader()
	} else {
		m.switchToFollower()
		m.electionState.electionTimer.startNewTerm()
	}
}

// gRPC stub implementations
// RequestVote is invoked by a secondary machine in the cluster thats trying to acquire a vote from this machine
func (m *GraftInstance[T]) RequestVote(ctx context.Context, args *pb.RequestVoteArgs) (*pb.RequestVoteResponse, error) {
	m.electionState.electionStateLock.Lock()
	m.logLock.Lock()
	defer m.electionState.electionStateLock.Unlock()
	defer m.logLock.Unlock()

	logUptoDate := m.log.entries[args.LastLogIndex].applicationTerm == int(args.LastLogTerm)

	if args.Term < m.electionState.currentTerm || !logUptoDate {
		return &pb.RequestVoteResponse{
			VoteGranted: false,
			CurrentTerm: m.electionState.currentTerm,
		}, nil
	}

	return &pb.RequestVoteResponse{
		VoteGranted: false,
		CurrentTerm: m.electionState.currentTerm,
	}, nil
}

// General utility + configuration parsing
// parseConfiguring parses a given configuration file
func parseConfiguration(configPath string) (time.Duration, map[int64]string) {
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(fmt.Errorf("failed to open yaml configuration: %w", err))
	}

	parsedYaml := make(map[interface{}]interface{})
	if err = yaml.Unmarshal(yamlFile, &parsedYaml); err != nil {
		panic(err)
	}

	clusterConfig := parsedYaml["configuration"].(map[int64]string)
	electionTimeoutBound, _ := time.ParseDuration(parsedYaml["duration"].(string))

	return electionTimeoutBound, clusterConfig
}
