package graft

import (
	"graft/pb"
)

type (
	// logEntry models an atomic operation that can be stored within the graft log
	logEntry[T any] struct {
		applicationTerm int64
		operation       T
	}

	// replicatedLog actually models the full log on the current machine in the raft cluster
	// + some additional metadata
	replicatedLog[T any] struct {
		serializer Serializer[T]
		entries    []logEntry[T]

		commitCallback func(T)
		lastCommitted  int
		lastApplied    int
	}
)

// newLog creates a Log for the machine in the raft cluster to consume
func newLog[T any](commitCallback func(T), serializer Serializer[T]) replicatedLog[T] {
	return replicatedLog[T]{
		entries:        []logEntry[T]{},
		commitCallback: commitCallback,
		serializer:     serializer,
		lastCommitted:  -1,
		lastApplied:    -1,
	}
}

// getPrevEntry returns the previous entry given a specific index
func (log *replicatedLog[T]) getPrevEntry(index int) logEntry[T] {
	if index-1 < 0 || index-1 >= len(log.entries) {
		return logEntry[T]{
			applicationTerm: -1,
		}
	}

	return log.entries[index-1]
}

// getLastEntry retrieves the head of the distributed log
func (log *replicatedLog[T]) getLastEntry() logEntry[T] {
	return log.getPrevEntry( /* index = */ len(log.entries))
}

// appendEntries duplicates all entries from a server onto the log
func (log *replicatedLog[T]) appendEntries(applicationIndex int, entries []*pb.LogEntry) {
	for i, entry := range entries {
		insertionIndex := i + applicationIndex
		parsedEntry := logEntry[T]{
			applicationTerm: entry.ApplicationTerm,
			operation:       log.serializer.FromString(entry.Entry),
		}

		if insertionIndex < len(log.entries) {
			// override existing entry
			log.entries[insertionIndex] = parsedEntry
		} else {
			// append to the end
			log.entries = append(log.entries, parsedEntry)
		}
	}
}

// updateCommitIndex updates the log's commit index by sequentially committing everything from the last commit index to the new commit index
func (log *replicatedLog[T]) updateCommitIndex(newCommitIndex int) {
	newCommitIndex = min(newCommitIndex, len(log.entries)-1)
	for _, op := range log.entries[log.lastCommitted+1 : newCommitIndex+1] {
		log.commitCallback(op.operation)
	}

	log.lastCommitted = newCommitIndex
}

// SerializeRange returns all entries in the log within a range but serialized using the serializer
// the goal is that they should be ready to transmit over gRPC
func (log *replicatedLog[T]) serializeRange(rangeStart int) []*pb.LogEntry {
	serializedResp := []*pb.LogEntry{}

	for _, entry := range log.entries[rangeStart:] {
		serializedResp = append(serializedResp, &pb.LogEntry{
			Entry:           log.serializer.ToString(entry.operation),
			ApplicationTerm: entry.applicationTerm,
		})
	}

	return serializedResp
}

// lastIndex returns the index in the log in which the current log's head sits
func (log *replicatedLog[T]) lastIndex() int64 {
	if len(log.entries) == 0 {
		return -1
	}

	return int64(len(log.entries) - 1)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
