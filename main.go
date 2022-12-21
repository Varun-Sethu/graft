package graft

import (
	"time"

	"gopkg.in/yaml.v3"
)

type (
	// Serializer "typeclass" in which an instance must be passed to graft on instantiation
	Serializer[T any] struct {
		ToString   func(T) string
		FromString func(string) T
	}

	graftConfig struct {
		clusterConfig           map[machineID]string
		electionTimeoutDuration time.Duration
	}
)

func parseGraftConfig(yamlConfig []byte) graftConfig {
	parsedYaml := make(map[interface{}]interface{})
	if err := yaml.Unmarshal(yamlConfig, &parsedYaml); err != nil {
		panic(err)
	}

	timeout := parsedYaml["election_timeout"].(string)
	clusterConfig := parsedYaml["cluster_configuration"].(map[machineID]string)

	duration, _ := time.ParseDuration(timeout)
	return graftConfig{
		clusterConfig:           clusterConfig,
		electionTimeoutDuration: duration,
	}
}
