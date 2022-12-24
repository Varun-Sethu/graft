package graft

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// These tests are really finnicky since they are highly time dependent
// just a heads up :D

func TestTriggersElection(t *testing.T) {
	bound := 10 * time.Millisecond

	timerInvoked := false
	timer := newElectionTimer(graftConfig{
		electionTimeoutDuration: bound,
	}, func() { timerInvoked = true })

	timer.start()
	time.Sleep(3 * bound)

	assert := assert.New(t)
	assert.True(timerInvoked)
}

func TestShouldProperlyDisableTimer(t *testing.T) {
	bound := 150 * time.Millisecond

	timerInvoked := false
	timer := newElectionTimer(graftConfig{
		electionTimeoutDuration: bound,
	}, func() { timerInvoked = true })

	assert := assert.New(t)

	// potential transient test failure
	timer.start()
	timer.stop()

	// sleep for "after" the timer, the timer should not be triggered
	time.Sleep(3 * bound)
	assert.False(timerInvoked)

	// restart it and sleep (under) the duration should not be triggered
	timer.start()
	time.Sleep(bound)
	assert.False(timerInvoked)

	// sleep longer, should be triggered
	time.Sleep(2 * bound)
	assert.True(timerInvoked)
}

func TestShouldNotPersistDeadTimers(t *testing.T) {
	// basically spin up and stop multiple different timers
	bound := 150 * time.Millisecond
	assert := assert.New(t)
	timerInvoked := false
	timer := newElectionTimer(graftConfig{
		electionTimeoutDuration: bound,
	}, func() { timerInvoked = true })

	for i := 0; i < 5; i++ {
		timer.start()
		timer.stop()

		time.Sleep(2 * bound)
		assert.False(timerInvoked)
	}
}

func TestResetShouldReset(t *testing.T) {
	bound := 150 * time.Millisecond
	assert := assert.New(t)

	timerInvoked := false
	timer := newElectionTimer(graftConfig{
		electionTimeoutDuration: bound,
	}, func() { timerInvoked = true })

	for i := 0; i < 5; i++ {
		timer.start()
		time.Sleep(bound / 2)
		timer.reset()
	}

	timer.stop()
	assert.False(timerInvoked)
}
