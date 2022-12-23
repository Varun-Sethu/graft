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

		isActive bool
		timer    *time.Timer

		// runElection should return a boolean indicating if the current machine
		// won the election
		runElection func()
	}
)

func newElectionTimer(config graftConfig, electionRunner func()) *electionTimer {
	return &electionTimer{
		electionTimeoutBound: config.electionTimeoutDuration,
		runElection:          electionRunner,
	}
}

// start actually starts the election timer and puts it in a state to begin triggering elections
func (timer *electionTimer) start() {
	timer.Lock()
	defer timer.Unlock()

	if timer.isActive {
		return
	}

	timer.currentElectionTimeout = getRandomDuration(timer.electionTimeoutBound)
	timer.timer = time.NewTimer(timer.currentElectionTimeout)
	timer.isActive = true

	go func() {
		// wait for the timer to end and rerun an election cycle afterwards
		for range timer.timer.C {
			timer.runElection()

			timer.Lock()
			if timer.isActive {
				timer.currentElectionTimeout = getRandomDuration(timer.electionTimeoutBound)
				timer.timer.Reset(timer.currentElectionTimeout)
			}
			timer.Unlock()
		}
	}()
}

// stops the timer countdown from occurring again
func (timer *electionTimer) stop() {
	timer.Lock()
	defer timer.Unlock()

	timer.isActive = false
}

// resets the timer countdown
func (timer *electionTimer) reset() {
	timer.Lock()
	defer timer.Unlock()

	timer.timer.Reset(timer.currentElectionTimeout)
}

func getRandomDuration(durationBound time.Duration) time.Duration {
	rand.Seed(time.Now().Unix())
	newTimeout := rand.Int63n(durationBound.Milliseconds()) + durationBound.Milliseconds()
	return time.Duration(newTimeout)
}
