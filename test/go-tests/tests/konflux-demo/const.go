package konflux_demo

import "time"

const (
	// Timeouts
	appDeployTimeout            = time.Minute * 20
	appRouteAvailableTimeout    = time.Minute * 5
	customResourceUpdateTimeout = time.Minute * 10
	jvmRebuildTimeout           = time.Minute * 40
	mergePRTimeout              = time.Minute * 1
	pipelineRunStartedTimeout   = time.Minute * 5
	pullRequestCreationTimeout  = time.Minute * 5
	releasePipelineTimeout      = time.Minute * 15
	snapshotTimeout             = time.Minute * 4
	releaseTimeout              = time.Minute * 4
	testPipelineTimeout         = time.Minute * 15
	branchCreateTimeout         = time.Minute * 1

	// Intervals
	defaultPollingInterval    = time.Second * 2
	jvmRebuildPollingInterval = time.Second * 10
	snapshotPollingInterval   = time.Second * 1
	releasePollingInterval    = time.Second * 1

	// test metadata
	devEnvTestLabel          = "konflux"
	upstreamKonfluxTestLabel = "upstream-konflux"
)
