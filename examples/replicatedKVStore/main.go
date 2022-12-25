package main

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"graft"
)

// Single machine in the replicated KV store cluster
var machineName, _ = strconv.Atoi(os.Getenv("MACHINE_NAME"))

func readClusterConfig() []byte {
	config, _ := ioutil.ReadFile("cluster_config.yaml")
	return config
}

func main() {
	var replicatedStore kvStore
	replicatedStore = kvStore{
		store: make(map[string]int),
		GraftInstance: graft.NewGraftInstance(
			/* configuration = */ readClusterConfig(),
			/* thisMachineID = */ int64(machineName),
			/* operationCallback = */ replicatedStore.applyOperation,
			/* serializer = */ graft.Serializer[kvStoreOperation]{
				ToString:   opToString,
				FromString: opFromString,
			},
		),
	}

	// handler for adding key value pairs to the replicated datastore
	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		key := r.Form.Get("key")
		value, _ := strconv.Atoi(r.Form.Get("value"))

		// push to the replicated store
		replicatedStore.ApplyOperation(kvStoreOperation{
			key:      key,
			newValue: value,
		})
	})

	// handler for querying a key value pair from the replicated datas
	http.HandleFunc("/query", func(w http.ResponseWriter, r *http.Request) {
		key := r.Form.Get("key")
		json, _ := json.Marshal(struct{ Value int }{
			Value: replicatedStore.store[key],
		})

		w.Header().Add("Content-Type", "application/json")
		w.Write(json)
	})

	// does not block, the server starts on port 8080
	replicatedStore.Start()
	http.ListenAndServe(":80", nil)
}
