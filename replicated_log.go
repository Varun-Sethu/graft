package graft

import (
	"graft/pb"
)

type (
	// LogEntry models an atomic operation that can be stored within the graft log
	LogEntry[T any] struct {
		applicationTerm int64
		operation       T
	}

	// Log actually models the full log on the current machine in the raft cluster
	// + some additional metadata
	Log[T any] struct {
		serializer Serializer[T]
		entries    []LogEntry[T]

		commitCallback func(T)
		lastCommitted  int
		lastApplied    int
	}
)

// newLog creates a Log for the machine in the raft cluster to consume
func newLog[T any](commitCallback func(T)) Log[T] {
	return Log[T]{
		entries:        []LogEntry[T]{},
		commitCallback: commitCallback,
		lastCommitted:  0,
		lastApplied:    0,
	}
}

// getLastEntry retrieves the head of the distributed log
func (log *Log[T]) getLastEntry() LogEntry[T] {
	return log.entries[len(log.entries)-1]
}

// appendEntries duplicates all entries from a server onto the log
func (log *Log[T]) appendEntries(entries []*pb.LogEntry, prevIndex int, prevTerm int64) {
	numNewEntries := len(log.entries) - 1 - prevIndex
	entryOverrides := entries[:numNewEntries]
	newEntries := entries[numNewEntries:]

	// first replace any overridden entries
	for i, v := range entryOverrides {
		indexToOverride := len(log.entries) - len(entryOverrides) + i

		log.entries[indexToOverride].applicationTerm = v.ApplicationTerm
		log.entries[indexToOverride].operation = log.serializer.FromString(v.Entry)
	}

	// then append any new entries
	for _, v := range newEntries {
		log.entries = append(log.entries, LogEntry[T]{
			applicationTerm: v.ApplicationTerm,
			operation:       log.serializer.FromString(v.Entry),
		})
	}
}

// updateCommitIndex updates the log's commit index by sequentially committing everything from the last commit index to the new commit index
func (log *Log[T]) updateCommitIndex(newCommitIndex int) {
	newCommitIndex = min(newCommitIndex, len(log.entries))
	for _, op := range log.entries[log.lastCommitted:newCommitIndex] {
		log.commitCallback(op.operation)
	}

	log.lastCommitted = newCommitIndex
}

// SerializeRange returns all entries in the log within a range but serialized using the serializer
// the goal is that they should be ready to transmit over gRPC
func (log *Log[T]) serializeSubset(rangeStart, rangeEnd int) []*pb.LogEntry {
	serializedResp := []*pb.LogEntry{}
	for _, entry := range log.entries[rangeStart:rangeEnd] {
		serializedResp = append(serializedResp, &pb.LogEntry{
			Entry:           log.serializer.ToString(entry.operation),
			ApplicationTerm: entry.applicationTerm,
		})
	}

	return serializedResp
}

// lastIndex returns the index in the log in which the current log's head sits
func (log *Log[T]) lastIndex() int64 {
	return int64(len(log.entries) - 1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
