package conformance

import "time"

const (
	// Timeouts
	customResourceUpdateTimeout = time.Minute * 10
	mergePRTimeout              = time.Minute * 1
	pipelineRunStartedTimeout   = time.Minute * 5
	pullRequestCreationTimeout  = time.Minute * 10
	snapshotTimeout             = time.Minute * 4
	releaseTimeout              = time.Minute * 4

	// conformanceCleanupMaxDuration caps how long AfterAll waits for namespace + Git
	// cleanup. After the budget, the suite still passes; override with E2E_CLEANUP_TIMEOUT
	// (Go duration string, e.g. 90s, 2m).
	conformanceCleanupMaxDuration = 2 * time.Minute

	// Intervals — keep these high enough to avoid GitHub API rate limits
	// when multiple concurrent test runs share the same GitHub App.
	defaultPollingInterval  = time.Second * 10
	snapshotPollingInterval = time.Second * 5
	releasePollingInterval  = time.Second * 5

	// test metadata
	devEnvTestLabel          = "konflux"
	upstreamKonfluxTestLabel = "upstream-konflux"
)
