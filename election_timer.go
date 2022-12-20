package graft

import (
	"math/rand"
	"sync"
	"time"
)

type (
	electionTimer struct {
		sync.Mutex

		currentTimerDuration time.Duration
		timer                *time.Timer
		isEnabled            bool

		getRandomDuration func() time.Duration
		invokeElection    func()
	}
)

func newElectionTimer(config graftConfig, electionInvokedCallback func()) *electionTimer {
	getRandomDuration := func() time.Duration {
		rand.Seed(time.Now().Unix())
		newTimeout := rand.Int63n(config.electionTimeoutDuration.Milliseconds()) + config.electionTimeoutDuration.Milliseconds()
		return time.Duration(newTimeout)
	}

	return &electionTimer{
		getRandomDuration: getRandomDuration,
		invokeElection:    electionInvokedCallback,
	}
}

// start actually starts the election timer and puts it in a state to begin triggering elections
func (timer *electionTimer) start() {
	timer.Lock()
	defer timer.Unlock()

	timer.currentTimerDuration = timer.getRandomDuration()
	timer.isEnabled = true
	timer.timer = time.AfterFunc(timer.currentTimerDuration, timer.triggerElection)
}

// triggerElection invokes the election handler and restarts the timer if required
func (timer *electionTimer) triggerElection() {
	timer.invokeElection()
	if timer.isEnabled {
		timer.start()
	}
}

// disable turns off the entire timer, this is primarily used when moving from
// follower to leader mode
func (timer *electionTimer) disable() {
	timer.Lock()
	defer timer.Unlock()

	timer.isEnabled = false
	timer.timer.Stop()
}
