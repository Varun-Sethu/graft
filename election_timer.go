package graft

import (
	"math/rand"
	"sync"
	"time"
)

// module for triggering events when the election timer is over
type (
	electionTimer struct {
		// electionTimeoutBound is a bound on how long our timeout for an election can last
		// electionTimeouts can be in the range [electionTimeoutBound, 2 * electionTimeoutBound)
		electionTimeoutBound time.Duration

		timer     *time.Timer
		timerLock sync.Mutex

		resetChannel   chan struct{}
		triggerNewTerm func()
	}
)

func newElectionTimer(timeoutBound time.Duration, triggerNewTerm func()) *electionTimer {
	return &electionTimer{
		electionTimeoutBound: timeoutBound,
		triggerNewTerm:       triggerNewTerm,
		resetChannel:         make(chan struct{}),
		timerLock:            sync.Mutex{},
	}
}

// starts the election timer
func (t *electionTimer) start() {
	electionTimeout := t.getNewElectionTimeout()
	t.timer = time.AfterFunc(electionTimeout, func() {
		t.startNewTerm()
		t.triggerNewTerm()
	})

	// the main timer loop
	go func() {
		for range t.resetChannel {
			t.timerLock.Lock()
			t.timer.Reset(electionTimeout)
			t.timerLock.Unlock()
		}
	}()
}

// triggers a new raft term
func (t *electionTimer) startNewTerm() {
	t.timerLock.Lock()
	defer t.timerLock.Unlock()

	newTimeout := t.getNewElectionTimeout()
	t.timer.Reset(newTimeout)
}

func (t *electionTimer) getNewElectionTimeout() time.Duration {
	rand.Seed(time.Now().Unix())
	newTimeout := rand.Int63n(t.electionTimeoutBound.Milliseconds()) + t.electionTimeoutBound.Milliseconds()

	return time.Duration(newTimeout)
}
