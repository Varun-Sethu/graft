package graft

import (
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	"graft/pb"

	"gopkg.in/yaml.v3"
)

const NO_VOTE = -1

type (
	GraftInstance[T any] struct {
		persistentState[T]
		leaderState

		isLeader         bool
		electionTimer    *electionTimer
		operationHandler func(T)
		cluster          map[int]pb.GraftClient
	}

	// persistentState is persistent state for each machine in the cluster
	persistentState[T any] struct {
		currentTerm       int
		electionCycleLock sync.Mutex

		votedFor int // the candidate that this machine voted for in the current term default nil
		log      Log[T]
	}

	// leaderState is volatile state that the machine maintains when elected leader
	leaderState struct {
		nextIndex  map[int]int
		matchIndex map[int]int
	}
)

// CreateGraftInstance creates a global graft instance thats ready to be spun up :)
// configPath is the path to the yaml file that contains the configuration for this raft cluster
// operationHandler is the handler invoked when the distributed log has committed an operation
func CreateGraftInstance[T any](operationHandler func(T)) *GraftInstance[T] {
	return &GraftInstance[T]{
		isLeader:         false,
		operationHandler: operationHandler,

		leaderState: leaderState{
			nextIndex:  make(map[int]int),
			matchIndex: make(map[int]int),
		},

		persistentState: persistentState[T]{
			currentTerm:       0,
			electionCycleLock: sync.Mutex{},
			votedFor:          NO_VOTE,
			log:               NewLog[T](),
		},
	}
}

// parseConfiguring parses a given configuration file
func parseConfiguration(configPath string) (time.Duration, map[int]string) {
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(fmt.Errorf("failed to open yaml configuration: %w", err))
	}

	parsedYaml := make(map[interface{}]interface{})
	if err = yaml.Unmarshal(yamlFile, &parsedYaml); err != nil {
		panic(err)
	}

	clusterConfig := parsedYaml["configuration"].(map[int]string)
	electionTimeoutBound, _ := time.ParseDuration(parsedYaml["duration"].(string))

	return electionTimeoutBound, clusterConfig
}

// InitFromConfig initializes the graft instance by parsing the provided config, starting an RPC server and then connecting to
// the RPC servers for the other members of the cluster
func (m *GraftInstance[T]) InitFromConfig(configPath string, machineId int) {
	m.startRPCServer()

	electionTimeoutBound, clusterConfig := parseConfiguration(configPath)
	cluster := make(map[int]pb.GraftClient)

	// dial each other member in the cluster
	for memberId, addr := range clusterConfig {
		if memberId != machineId {
			cluster[memberId] = connectToClusterMember(addr)
			m.matchIndex[memberId] = -1
			m.nextIndex[memberId] = -1
		}
	}

	// construct the election timer and the leadership state
	m.electionTimer = newElectionTimer(electionTimeoutBound, func() {})
	m.cluster = cluster
}

// switchToLeader toggles a machine as the new leader of the cluster
// SwitchToFollower is the opposite direction
// startNewTerm toggles a new term in the raft cycle, note that the electionCycleLock must be acquired first
func (m *GraftInstance[T]) switchToLeader()   { m.isLeader = true }
func (m *GraftInstance[T]) switchToFollower() { m.isLeader = false }
func (m *GraftInstance[T]) startNewTerm() {
	m.currentTerm += 1
	m.votedFor = NO_VOTE
}

// tryVoteFor attempts to acquire a vote from this machine for {candidateID}
// given their the last index in their log (lastLogIndex) and the term in which that
// entry was added (lastLogTerm) and given that their current view of the term is (candidateTerm)
func (m *GraftInstance[T]) tryVoteFor(candidateId, candidateTerm, lastLogIndex, lastLogTerm int) bool {
	m.electionCycleLock.Lock()
	defer m.electionCycleLock.Unlock()

	if candidateTerm < m.currentTerm || m.votedFor != NO_VOTE {
		return false
	}

	logHead := m.log.GetHead()
	candidateLogUpToDate := lastLogTerm >= logHead.applicationTerm && lastLogIndex >= len(m.log.entries)-1
	if candidateLogUpToDate {
		m.votedFor = candidateId
		return true
	}

	return false
}

// starts an election cycle
func (m *GraftInstance[T]) triggerElection() {
	m.electionCycleLock.Lock()
	defer m.electionCycleLock.Unlock()

	// start new term and vote for yourself
	m.startNewTerm()
}
