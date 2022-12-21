package graft

import (
	"sync"

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
		sync.Mutex

		serializer    Serializer[T]
		entries       []LogEntry[T]
		lastCommitted int
		lastApplied   int
	}
)

// NewLog creates a Log for the machine in the raft cluster to consume
func NewLog[T any]() Log[T] {
	return Log[T]{
		entries:       []LogEntry[T]{},
		lastCommitted: 0,
		lastApplied:   0,
	}
}

// GetHead retrieves the head of the distributed log
func (log *Log[T]) GetHead() LogEntry[T] {
	return log.entries[len(log.entries)-1]
}

// ApplyEntries duplicates all entries from a server onto the log
func (log *Log[T]) ApplyEntries(entries []*pb.LogEntry, prevIndex int, prevTerm int64) {
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

// SerializeRange returns all entries in the log within a range but serialized using the serializer
// the goal is that they should be ready to transmit over gRPC
func (log *Log[T]) SerializeSubset(rangeStart int) []*pb.LogEntry {
	serializedResp := []*pb.LogEntry{}
	for _, entry := range log.entries[rangeStart:] {
		serializedResp = append(serializedResp, &pb.LogEntry{
			Entry:           log.serializer.ToString(entry.operation),
			ApplicationTerm: entry.applicationTerm,
		})
	}

	return serializedResp
}

// HeadIndex returns the index in the log in which the current log's head sits
func (log *Log[T]) HeadIndex() int64 {
	return int64(len(log.entries) - 1)
}
