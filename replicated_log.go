package graft

// LogEntry models an atomic operation that can be stored within the graft log
type LogEntry[T any] struct {
	applicationTerm int64
	operation       T
}

// Log actually models the full log on the current machine in the raft cluster
// + some additional metadata
type Log[T any] struct {
	entries       []LogEntry[T]
	lastCommitted int
	lastApplied   int
}

// NewLog creates a Log for the machine in the raft cluster to consume
func NewLog[T any]() Log[T] {
	return Log[T]{
		entries:       []LogEntry[T]{},
		lastCommitted: 0,
		lastApplied:   0,
	}
}

// GetHead retrieves the head of the distributed log
func (log Log[T]) GetHead() LogEntry[T] {
	return log.entries[len(log.entries)-1]
}

// HeadIndex returns the index in the log in which the current log's head sits
func (log Log[T]) HeadIndex() int64 {
	return int64(len(log.entries) - 1)
}
