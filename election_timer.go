package graft

import (
	"math/rand"
	"sync"
	"time"
)

// module for triggering events when the election timer is over
type (
	triggerElectionTimer struct {
		// electionTimeoutBound is a bound on how long our timeout for an election can last
		// electionTimeouts can be in the range [electionTimeoutBound, 2 * electionTimeoutBound)
		electionTimeoutBound        time.Duration
		currentElectTimeoutDuration time.Duration

		timer     *time.Timer
		timerLock sync.Mutex

		enabled                 bool
		enableLock              sync.Mutex
		resetChannel            chan struct{}
		electionTriggerCallback func()
	}
)

// constructor for election timers
func newElectionTimer(timeoutBound time.Duration, electionTriggerCallback func()) *triggerElectionTimer {
	return &triggerElectionTimer{
		electionTimeoutBound:    timeoutBound,
		electionTriggerCallback: electionTriggerCallback,
		resetChannel:            make(chan struct{}),
		timerLock:               sync.Mutex{},
	}
}

// start starts the election timer, also used to restart a timer after its been disabled
func (t *triggerElectionTimer) start() {
	electionTimeout := getRandomTimeoutDuration(t.electionTimeoutBound)
	t.currentElectTimeoutDuration = electionTimeout
	t.timer = time.AfterFunc(electionTimeout, t.triggerElection)
}

// triggerElection triggers a new raft election by resetting the timer and invoking the election callback
func (t *triggerElectionTimer) triggerElection() {
	t.timerLock.Lock()
	defer t.timerLock.Unlock()

	t.timer.Stop()
	t.electionTriggerCallback()

	t.enableLock.Lock()
	defer t.enableLock.Unlock()
	if t.enabled {
		t.start()
	}
}

// disable triggers the timer to move as if it was under a leader
// ie. do nothing :)
func (t *triggerElectionTimer) disable() {
	t.enableLock.Lock()
	defer t.enableLock.Unlock()
	t.enabled = false
}

// reset resets the election timer completely
func (t *triggerElectionTimer) reset() {
	t.timerLock.Lock()
	defer t.timerLock.Unlock()

	t.timer.Reset(t.currentElectTimeoutDuration)
}

// getRandomTimeoutDuration generates a randomized timeout duration
func getRandomTimeoutDuration(timeoutBound time.Duration) time.Duration {
	rand.Seed(time.Now().Unix())
	newTimeout := rand.Int63n(int64(timeoutBound.Milliseconds())) + timeoutBound.Milliseconds()

	return time.Duration(newTimeout)
}
