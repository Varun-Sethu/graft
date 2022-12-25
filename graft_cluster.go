package graft

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"graft/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
		machineCancellationFuncs sync.Map // equiv to: map[machineID]context.CancelFunc
		currentLeader            machineID
	}
)

// newCluster creates a cluster struct and connects to all other machines in the cluster
func connectToCluster(config graftConfig, thisMachinesID machineID) *cluster {
	newCluster := cluster{
		machines:                 make(map[machineID]lazyClient),
		machineCancellationFuncs: sync.Map{},
		currentLeader:            unknownLeader,
	}

	for machineID, machineAddr := range config.clusterConfig {
		if machineID == thisMachinesID {
			continue
		}

		newCluster.machines[machineID] = connectToMachine(machineAddr)
		newCluster.machineCancellationFuncs.Store(machineID, context.CancelFunc(func() {}))
	}

	return &newCluster
}

// pushEntries pushes entries to all entities within the cluster, primarily abstracts away any error handling involved with the
// invocation of this append entries RPC, ie cancelling existing requests and timing out old ones
func (c *cluster) appendEntryForMember(machineID machineID, entry *pb.AppendEntriesArgs, currentTerm int64) *pb.AppendEntriesResponse {
	// cancel any outbound request and create a new one
	cancelExistingReqForMachine, _ := c.machineCancellationFuncs.Load(machineID)
	(cancelExistingReqForMachine.(context.CancelFunc))()

	machineContext, cancelFunc := context.WithCancel(context.Background())
	c.machineCancellationFuncs.Store(machineID, cancelFunc)
	clusterMachine := c.machines[machineID]

	ctx, cancel := context.WithTimeout(machineContext, 100*time.Millisecond)
	defer cancel()

	result, err := clusterMachine().AppendEntries(ctx, entry)
	if err != nil {
		return nil
	}

	return result
}

// requestVote requests a vote from each member of the cluster and accumulates the total
// returns the total vote and the current term
func (c *cluster) requestVote(voteRequest *pb.RequestVoteArgs) (int, int) {
	termLock := sync.Mutex{}
	newTerm := -1

	totalVotes := int32(0)

	// voting wait group
	wg := sync.WaitGroup{}
	wg.Add(len(c.machines))

	for _, clusterMachine := range c.machines {
		// poll each machine in the cluster for a vote
		go func(clusterMachine pb.GraftClient) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			defer wg.Done()

			voteResult, err := clusterMachine.RequestVote(ctx, voteRequest)
			if err != nil {
				return
			}

			if voteResult.VoteGranted {
				atomic.AddInt32(&totalVotes, 1)
			}

			termLock.Lock()
			if voteResult.CurrentTerm > int64(newTerm) {
				newTerm = int(voteResult.CurrentTerm)
			}
			termLock.Unlock()
		}(clusterMachine())
	}

	wg.Wait()
	return int(totalVotes), newTerm
}

// pushOperationToLeader propagates an operation to the global cluster leader
func (c *cluster) pushOperationToLeader(serializedOperation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)

	go func() {
		c.machines[c.currentLeader]().AddLogEntry(ctx, &pb.AddLogEntryArgs{
			Operation: serializedOperation,
		})
		cancel()
	}()
}

func (c *cluster) clusterSize() int { return len(c.machines) }

// connects to a machine (lazily), ie the connection is only established when it is first used
func connectToMachine(addr string) lazyClient {
	var v pb.GraftClient
	var once sync.Once

	return func() pb.GraftClient {
		once.Do(func() {
			opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

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
