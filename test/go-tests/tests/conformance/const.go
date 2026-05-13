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

	// conformanceCleanupMaxDuration caps how long AfterAll waits for namespace + Git
	// cleanup. After the budget, the suite still passes; override with E2E_CLEANUP_TIMEOUT
	// (Go duration string, e.g. 90s, 2m).
	conformanceCleanupMaxDuration = 2 * time.Minute

	// Intervals
	defaultPollingInterval  = time.Second * 2
	snapshotPollingInterval = time.Second * 1
	releasePollingInterval  = time.Second * 1

	// test metadata
	devEnvTestLabel          = "konflux"
	upstreamKonfluxTestLabel = "upstream-konflux"

	// default-tenant operator wiring (internal registry pull secret on integration runner SA)
	defaultTenantNamespace            = "default-tenant"
	konfluxIntegrationRunnerSAName    = "konflux-integration-runner"
	defaultTenantInternalRegistryCred = "regcred-internal-registry"
)
