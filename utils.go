package graft

import (
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/yaml.v3"
)

// General utility + configuration parsing
// parseConfiguring parses a given configuration file
func parseConfiguration(configPath string) (time.Duration, map[int64]string) {
	yamlFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		panic(fmt.Errorf("failed to open yaml configuration: %w", err))
	}

	parsedYaml := make(map[interface{}]interface{})
	if err = yaml.Unmarshal(yamlFile, &parsedYaml); err != nil {
		panic(err)
	}

	clusterConfig := parsedYaml["configuration"].(map[int64]string)
	electionTimeoutBound, _ := time.ParseDuration(parsedYaml["duration"].(string))

	return electionTimeoutBound, clusterConfig
}
