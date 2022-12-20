package graft

import (
	"context"
	"fmt"
	"io/ioutil"
	"sync"
	"sync/atomic"
	"time"

	"graft/pb"

	"google.golang.org/grpc"
	"gopkg.in/yaml.v3"
)

// models an actual cluster of graft instances and provides abstractions for interfacing with all of them
const unknownLeader = 0

type (
	machineID uint64
	cluster   struct {
		// machines are the gRPC clients for all the machines in the cluster
		// while machineContext maps machines to any active cancelable contexts
		machines                 map[machineID]pb.GraftClient
		machineCancellationFuncs map[machineID]context.CancelFunc
		currentLeader            machineID
	}
)

// clusterFromConfig creates a cluster struct and connects to all other machines in the cluster
func clusterFromConfig(thisMachinesID machineID, graftConfigPath string) *cluster {
	clusterConfiguration := parseClusterConfig(graftConfigPath)
	newCluster := cluster{
		machines:                 make(map[machineID]pb.GraftClient),
		machineCancellationFuncs: make(map[machineID]context.CancelFunc),
		currentLeader:            unknownLeader,
	}

	for machineID, machineAddr := range clusterConfiguration {
		if machineID == thisMachinesID {
			continue
		}

		newCluster.machines[machineID] = connectToMachine(machineAddr)
		newCluster.machineCancellationFuncs[machineID] = func() {}
	}

	return &newCluster
}

// pushEntries pushes entries to all entities within the cluster
func (c *cluster) pushEntryToClusterMember(machineID machineID, entry *pb.AppendEntriesArgs) {
	// cancel any outbound request and create a new one
	cancelExistingReq := c.machineCancellationFuncs[machineID]

	cancelExistingReq()
	machineContext, cancelFunc := context.WithCancel(context.Background())
	c.machineCancellationFuncs[machineID] = cancelFunc

	clusterMachine := c.machines[machineID]
	timedOut := func(ctx context.Context) bool {
		select {
		case <-ctx.Done():
			return true
		default:
			return false
		}
	}

	go func() {
		for {
			ctx, cancel := context.WithTimeout(machineContext, 10*time.Millisecond)
			defer cancel()
			clusterMachine.AppendEntries(ctx, entry)

			// if we timed out resend the request, otherwise exist this loop
			if !timedOut(ctx) {
				break
			}
		}
	}()
}

// requestVote requests a vote from each member of the cluster and accumulates the total
func (c *cluster) requestVote(voteRequest *pb.RequestVoteArgs) int {
	totalVotes := int32(0)

	// voting wait group
	wg := sync.WaitGroup{}
	wg.Add(len(c.machines))

	for _, clusterMachine := range c.machines {
		// poll each machine in the cluster for a vote
		go func(clusterMachine pb.GraftClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			voteResult, _ := clusterMachine.RequestVote(ctx, voteRequest)
			if voteResult.VoteGranted {
				atomic.AddInt32(&totalVotes, 1)
			}

			wg.Done()
		}(clusterMachine)
	}

	wg.Wait()
	return int(totalVotes)
}

// cancelAllOutboundReqs terminates every single outbound request to any machine in the cluster
// this is primarily used when switching state from leader to follower
func (c *cluster) cancelAllOutboundReqs() {
	for _, cancelExistingReq := range c.machineCancellationFuncs {
		cancelExistingReq()
	}
}

// parseClusterConfig parses the cluster_configuration of the graft config file
// it looks something like:
// election_timeout: 150
// cluster_configuration:
//   1: tomato
//   2: gamma
func parseClusterConfig(graftConfigPath string) map[machineID]string {
	yamlFile, err := ioutil.ReadFile(graftConfigPath)
	if err != nil {
		panic(fmt.Errorf("failed to open yaml configuration: %w", err))
	}

	parsedYaml := make(map[interface{}]interface{})
	if err = yaml.Unmarshal(yamlFile, &parsedYaml); err != nil {
		panic(err)
	}

	clusterConfig := parsedYaml["cluster_configuration"].(map[machineID]string)
	return clusterConfig
}

func connectToMachine(addr string) pb.GraftClient {
	opts := []grpc.DialOption{}

	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		panic(err)
	}

	// TODO: close the conn eventually
	return pb.NewGraftClient(conn)
}
