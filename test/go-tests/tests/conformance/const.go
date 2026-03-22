package conformance

import "time"

const (
	// Timeouts
	customResourceUpdateTimeout = time.Minute * 10
	mergePRTimeout              = time.Minute * 1
	pipelineRunStartedTimeout   = time.Minute * 5
	pullRequestCreationTimeout  = time.Minute * 5
	releasePipelineTimeout      = time.Minute * 15
	snapshotTimeout             = time.Minute * 4
	releaseTimeout              = time.Minute * 4

	// Intervals
	defaultPollingInterval  = time.Second * 2
	snapshotPollingInterval = time.Second * 1
	releasePollingInterval  = time.Second * 1

	// test metadata
	devEnvTestLabel          = "konflux"
	upstreamKonfluxTestLabel = "upstream-konflux"
)
