package main

import (
	"encoding/json"

	"graft"
)

type (
	kvStore struct {
		store map[string]int
		*graft.GraftInstance[kvStoreOperation]
	}

	kvStoreOperation struct {
		key      string
		newValue int
	}
)

// applyOperation takes a kvStore and applies some operation to it
func (store *kvStore) applyOperation(operation kvStoreOperation) {
	store.store[operation.key] = operation.newValue
}

func opToString(op kvStoreOperation) string {
	marshalledJson, _ := json.Marshal(op)
	return string(marshalledJson)
}

func opFromString(op string) kvStoreOperation {
	result := kvStoreOperation{}
	json.Unmarshal([]byte(op), &result)

	return result
}
