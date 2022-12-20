package graft

import (
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/yaml.v3"
)

type graftConfig struct {
	clusterConfig           map[machineID]string
	electionTimeoutDuration time.Duration
}

func parseGraftConfig(graftConfigPath string) graftConfig {
	yamlFile, err := ioutil.ReadFile(graftConfigPath)
	if err != nil {
		panic(fmt.Errorf("failed to open yaml configuration: %w", err))
	}

	parsedYaml := make(map[interface{}]interface{})
	if err = yaml.Unmarshal(yamlFile, &parsedYaml); err != nil {
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
