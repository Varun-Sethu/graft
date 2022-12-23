package graft

import (
	"math/rand"
	"sync"
	"time"
)

type (
	electionTimer struct {
		sync.Mutex

		electionTimeoutBound   time.Duration
		currentElectionTimeout time.Duration
		timer                  *time.Timer

		// runElection should return a boolean indicating if the current machine
		// won the election
		runElection func() bool
	}
)

func newElectionTimer(config graftConfig, electionRunner func() bool) *electionTimer {
	return &electionTimer{
		electionTimeoutBound: config.electionTimeoutDuration,
		runElection:          electionRunner,
	}
}

// start actually starts the election timer and puts it in a state to begin triggering elections
func (timer *electionTimer) start() {
	// todo: figure out how to resolve this when called multiple times

	timer.Lock()
	timer.currentElectionTimeout = getRandomDuration(timer.electionTimeoutBound)
	timer.timer = time.NewTimer(timer.currentElectionTimeout)
	timer.Unlock()

	go func() {
		// wait for the timer to end and rerun an election cycle afterwards
		for range timer.timer.C {
			wonElection := timer.runElection()
			if !wonElection {
				timer.Lock()
				timer.currentElectionTimeout = getRandomDuration(timer.electionTimeoutBound)
				timer.timer.Reset(timer.currentElectionTimeout)
				timer.Unlock()
			}
		}
	}()
}

// resets the timer countdown
func (timer *electionTimer) resetTimer() {
	timer.Lock()
	defer timer.Unlock()

	timer.timer.Reset(timer.currentElectionTimeout)
}

func getRandomDuration(durationBound time.Duration) time.Duration {
	rand.Seed(time.Now().Unix())
	newTimeout := rand.Int63n(durationBound.Milliseconds()) + durationBound.Milliseconds()
	return time.Duration(newTimeout)
}
