# graft
Really wacky Go implementation of the Raft distributed consensus algorithm. Please don't actually use this :)

## Usage
If you do actually want to use this though (even though you shouldn't) usage is very simple. Clusters are configured via a yaml file, this yaml file contains information regarding the configured
election timeout and all the machines + addresses of those machines in the cluster. Currently there is no support for handling changes to cluster configurations properly but that will come soon :). An example configuration may look like:
```yaml
# electionTimeout is measured in miliseconds, this should be configured based on the average request time between machines in the cluster
electionTimeout: 150
configuration:
  1: tomato
  2: gamma
```

Like most raft implementations this library is designed to expose a small consensus module that you embed within the type you wish to have replicated. For example we may wish to define a simple replicated key-value store for some important configuration of a system. Such a store would use graft as follows:
```go
type (
    KVStore struct {
        store map[string]string
        // embedded consensus module
        GraftInstance[KVOperation]
    }

    KVOperation struct {
        key   string
        value string
    }
)

// operation applicator function that you must define for graft to work
func (kv KVStore) applicator(operation KVOperation) {
    kv.store[operation.key] = operation.value
}

// operation serializer and deserializers
func serialize(data KVOperation) string { /* ... */ }
func deserialize(data string) KVOperation { /* ... */ }

func main() {
    var replicatedStore KVStore
    replicatedStore = KVStore {
        store: make(map[string]string)
        GraftInstance: NewGraftInstance[KVOperation](
            /* configuration = */ PARSED_YAML_CONFIG,
            /* machineID = */ THIS_MACHINES_ID_IN_CONFIG,
            /* operationCommitCallback = */ replicatedStore.applicator,
            /* serializer = */ Serializer[KVOperation] {
                ToString: serialize,
                FromString: deserialize
            },
        )
    }

    // you can then apply operations to the entire graft cluster as follows
    replicatedStore.ApplyOperation(KVOperation {
        key: "hello",
        value: "world"
    })
}
```
A big chunk of the library is written to utilize generics as much as possible to expose a relatively clean interface without any empty interface :P. The `Serializer` struct is meant to mimic a haskell typeclass or a rust trait hence why its defined in a rather odd manner.

## Issues
The library doesn't actually persist anything at the moment :).