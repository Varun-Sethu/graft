package graft

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"graft/pb"

	"google.golang.org/grpc"
)

// models an actual cluster of graft instances and provides abstractions for interfacing with all of them
const unknownLeader = 0

type (
	machineID  = int64
	lazyClient func() pb.GraftClient
	cluster    struct {
		// machines are the gRPC clients for all the machines in the cluster
		// while machineContext maps machines to any active cancelable contexts
		machines                 map[machineID]lazyClient
		machineCancellationFuncs map[machineID]context.CancelFunc
		currentLeader            machineID
	}
)

// newCluster creates a cluster struct and connects to all other machines in the cluster
func connectToCluster(config graftConfig, thisMachinesID machineID) *cluster {
	newCluster := cluster{
		machines:                 make(map[machineID]lazyClient),
		machineCancellationFuncs: make(map[machineID]context.CancelFunc),
		currentLeader:            unknownLeader,
	}

	for machineID, machineAddr := range config.clusterConfig {
		if machineID == thisMachinesID {
			continue
		}

		newCluster.machines[machineID] = connectToMachine(machineAddr)
		newCluster.machineCancellationFuncs[machineID] = func() {}
	}

	return &newCluster
}

// pushEntries pushes entries to all entities within the cluster
// as per the raft specification this will retry the request indefinitely until it is cancelled
func (c *cluster) pushEntryToClusterMember(machineID machineID, entry *pb.AppendEntriesArgs) *pb.AppendEntriesResponse {
	// cancel any outbound request and create a new one
	cancelExistingReq := c.machineCancellationFuncs[machineID]
	cancelExistingReq()

	machineContext, cancelFunc := context.WithCancel(context.Background())
	c.machineCancellationFuncs[machineID] = cancelFunc
	clusterMachine := c.machines[machineID]

	// repeatedly try sending a request
	for {
		ctx, cancel := context.WithTimeout(machineContext, 100*time.Millisecond)
		defer cancel()
		result, _ := clusterMachine().AppendEntries(ctx, entry)

		// if we timed out resend the request, otherwise exist this loop
		timedOut := ctx.Err() == context.DeadlineExceeded
		if !timedOut {
			return result
		}

		cancel()
	}
}

// requestVote requests a vote from each member of the cluster and accumulates the total
// returns the total vote and the current term
func (c *cluster) requestVote(voteRequest *pb.RequestVoteArgs) (int, int) {
	totalVotes := int32(0)
	termLock := sync.Mutex{}
	newTerm := 0

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

			termLock.Lock()
			if voteResult.CurrentTerm > int64(newTerm) {
				newTerm = int(voteResult.CurrentTerm)
			}
			termLock.Unlock()

			wg.Done()
		}(clusterMachine())
	}

	wg.Wait()
	return int(totalVotes), newTerm
}

// cancelAllOutboundReqs terminates every single outbound request to any machine in the cluster
// this is primarily used when switching state from leader to follower
func (c *cluster) cancelAllOutboundReqs() {
	for _, cancelExistingReq := range c.machineCancellationFuncs {
		cancelExistingReq()
	}
}

func (c *cluster) clusterSize() int { return len(c.machines) }

// connects to a machine (lazily), ie the connection is only established when it is first used
func connectToMachine(addr string) lazyClient {
	var v pb.GraftClient
	var once sync.Once

	return func() pb.GraftClient {
		once.Do(func() {
			opts := []grpc.DialOption{}

			conn, err := grpc.Dial(addr, opts...)
			if err != nil {
				panic(err)
			}

			// TODO: close the conn eventually
			v = pb.NewGraftClient(conn)
		})

		return v
	}
}
