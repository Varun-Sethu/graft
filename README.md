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

The library is designed to "sit beneath your application layer" and just work, once you have configured your cluster you can start an instance of graft as follows:
```go
const (
    GRAFT_CONFIG = "example_config.yaml"
)

type StateMachineOperation struct {
    // ... model your state machine operation here
}

// this function will be invoked by graft whenever an operation is committed to the log
// ideally this function is what will end up 
func operationCallback(op StateMachineOperation) {}

func main() {
    graftInstance := NewGraftInstance[StateMachineOperation](operationCallback)

    // start up the RPC server for this member and connect to the other members in the cluster
    // non-blocking
    graftInstance.StartFromConfig(GRAFT_CONFIG, machineId)
}
```
I required there are some examples within the `examples/` folder, the main one being a replicated key-value store. In the future I will add a sharded key-value store.

## Issues
The library doesn't actually persist anything at the moment :).