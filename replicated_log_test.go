package graft

import (
	"testing"

	"graft/pb"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type (
	testOperation struct {
		uniqueID uuid.UUID
	}
)

func TestUpdatesLastEntryCorrectly(t *testing.T) {
	callback := func(testOperation) {}
	log := newLog(
		/* commitCallback = */ callback,
		/* serializer = */ Serializer[testOperation]{
			ToString:   func(to testOperation) string { return "" },
			FromString: func(s string) testOperation { return testOperation{} },
		},
	)

	// should return some default state on empty logs
	assert := assert.New(t)
	assert.Equal(logEntry[testOperation]{
		applicationTerm: -1,
	}, log.getLastEntry())

	assert.Equal(int64(-1), log.lastIndex())

	// return proper head values when new entries are pushed
	firstOperationID := uuid.New()
	secondOperationID := uuid.New()

	firstInsertedOp := logEntry[testOperation]{
		applicationTerm: 1,
		operation: testOperation{
			uniqueID: firstOperationID,
		},
	}

	secondInsertedOp := logEntry[testOperation]{
		applicationTerm: 2,
		operation: testOperation{
			uniqueID: secondOperationID,
		},
	}

	log.entries = append(log.entries, firstInsertedOp)
	assert.Equal(int64(0), log.lastIndex())
	assert.Equal(firstInsertedOp, log.getLastEntry())

	log.entries = append(log.entries, secondInsertedOp)
	assert.Equal(int64(1), log.lastIndex())
	assert.Equal(secondInsertedOp, log.getLastEntry())
}

func TestSerializesCorrectRange(t *testing.T) {
	callback := func(testOperation) {}
	log := newLog(
		/* commitCallback = */ callback,
		/* serializer = */ Serializer[testOperation]{
			ToString:   func(to testOperation) string { return to.uniqueID.String() },
			FromString: func(s string) testOperation { return testOperation{} },
		},
	)

	assert := assert.New(t)

	firstOperationID := uuid.New()
	secondOperationID := uuid.New()
	log.entries = append(log.entries, []logEntry[testOperation]{
		{
			applicationTerm: 1,
			operation: testOperation{
				uniqueID: firstOperationID,
			},
		},
		{
			applicationTerm: 2,
			operation: testOperation{
				uniqueID: secondOperationID,
			},
		},
	}...)

	assert.Equal([]string{firstOperationID.String(), secondOperationID.String()}, transformLogEntries(log.serializeRange( /* rangeStart = */ 0)))
	assert.Equal([]string{secondOperationID.String()}, transformLogEntries(log.serializeRange( /* rangeStart = */ 1)))
}

func TestSuccessfullyAppendsEntries(t *testing.T) {
	callback := func(testOperation) {}
	log := newLog(
		/* commitCallback = */ callback,
		/* serializer = */ Serializer[testOperation]{
			ToString: func(to testOperation) string { return to.uniqueID.String() },
			FromString: func(s string) testOperation {
				return testOperation{
					uniqueID: uuid.MustParse(s),
				}
			},
		},
	)

	initEntries := []*pb.LogEntry{
		{
			ApplicationTerm: 0,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 1,
			Entry:           uuid.NewString(),
		},
	}

	log.appendEntries(0, initEntries)

	assert := assert.New(t)
	// append entries from the start
	assert.Equal(int64(0), log.entries[0].applicationTerm)
	assert.Equal(int64(1), log.entries[1].applicationTerm)

	newEntries := []*pb.LogEntry{
		{
			ApplicationTerm: 2,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 3,
			Entry:           uuid.NewString(),
		},
	}

	// both append and override entries
	log.appendEntries(1, newEntries)
	assert.Equal(int64(2), log.entries[1].applicationTerm)
	assert.Equal(int64(3), log.entries[2].applicationTerm)

	// append completely new entries
	log.appendEntries(3, newEntries)
	assert.Equal(int64(2), log.entries[3].applicationTerm)
	assert.Equal(int64(3), log.entries[4].applicationTerm)

	// override entirely existing entries
	log.appendEntries(0, newEntries)
	assert.Equal(int64(2), log.entries[0].applicationTerm)
	assert.Equal(int64(3), log.entries[1].applicationTerm)
}

func TestUpdateCommitIndexPropagatesCommits(t *testing.T) {
	commitCount := 0
	callback := func(testOperation) { commitCount++ }
	log := newLog(
		/* commitCallback = */ callback,
		/* serializer = */ Serializer[testOperation]{
			ToString: func(to testOperation) string { return to.uniqueID.String() },
			FromString: func(s string) testOperation {
				return testOperation{
					uniqueID: uuid.MustParse(s),
				}
			},
		},
	)

	// both append and override entries
	log.appendEntries(0, []*pb.LogEntry{
		{
			ApplicationTerm: 2,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 3,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 4,
			Entry:           uuid.NewString(),
		},
	})
	log.updateCommitIndex(100)

	log.appendEntries(3, []*pb.LogEntry{
		{
			ApplicationTerm: 2,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 3,
			Entry:           uuid.NewString(),
		},
		{
			ApplicationTerm: 4,
			Entry:           uuid.NewString(),
		},
	})
	log.updateCommitIndex(4)

	assert := assert.New(t)
	assert.Equal(5, commitCount)
}

func transformLogEntries(entries []*pb.LogEntry) []string {
	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Entry)
	}

	return result
}
