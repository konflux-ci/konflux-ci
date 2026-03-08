package common

import (
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	corev1 "k8s.io/api/core/v1"
)

// Test environments
const (
	DownstreamTestEnvironment string = "downstream"
	UpstreamTestEnvironment   string = "upstream"
)

const (
	ApplicationNameDefault       string = "appstudio"
	ReleaseStrategyPolicyDefault string = "mvp-policy"
	ReleaseStrategyPolicy        string = "policy"

	QuayTokenSecret                      string = "test-quay-token-secret"
	RedhatAppstudioUserSecret            string = "hacbs-release-tests-token"
	ReleaseCatalogTAQuaySecret           string = "release-catalog-trusted-artifacts-quay-secret"
	RedhatAppstudioQESecret              string = "redhat-appstudio-qe-bot-token"
	HacbsReleaseTestsTokenSecret         string = "redhat-appstudio-registry-pull-secret"
	PublicSecretNameAuth                 string = "cosign-public-key"
	ReleasePipelineServiceAccountDefault string = "release-service-account"

	SourceReleasePlanName          string = "source-releaseplan"
	SecondReleasePlanName          string = "the-second-releaseplan"
	TargetReleasePlanAdmissionName string = "demo"
	ReleasePvcName                 string = "release-pvc"
	ReleaseEnvironment             string = "production"

	ReleaseCreationTimeout              = 5 * time.Minute
	ReleasePipelineRunCreationTimeout   = 10 * time.Minute
	ReleasePipelineRunCompletionTimeout = 60 * time.Minute
	BuildPipelineRunCompletionTimeout   = 60 * time.Minute
	BuildPipelineRunCreationTimeout     = 10 * time.Minute
	ReleasePlanStatusUpdateTimeout      = 1 * time.Minute
	DefaultInterval                     = 100 * time.Millisecond
	PullRequestCreationTimeout          = 5 * time.Minute
	PipelineRunStartedTimeout           = 5 * time.Minute
	SnapshotTimeout                     = 4 * time.Minute
	SnapshotPollingInterval             = 1 * time.Second
	DefaultPollingInterval              = 2 * time.Second
	MergePRTimeout                      = 1 * time.Minute

	// Pipelines constants
	ComponentName                   string = "dc-metro-map"
	GitSourceComponentUrl           string = "https://github.com/redhat-appstudio-qe/dc-metro-map-release"
	AdditionalComponentName         string = "simple-python"
	AdditionalGitSourceComponentUrl string = "https://github.com/redhat-appstudio-qe/devfile-sample-python-basic-test2"
	PyxisStageImagesApiEndpoint     string = "https://pyxis.preprod.api.redhat.com/v1/images/id/"
	GitLabRunFileUpdatesTestRepo    string = "https://gitlab.cee.redhat.com/hacbs-release-tests/app-interface"

	// EC constants
	EcPolicyLibPath     = "github.com/enterprise-contract/ec-policies//policy/lib"
	EcPolicyReleasePath = "github.com/enterprise-contract/ec-policies//policy/release"
	EcPolicyDataBundle  = "oci::quay.io/konflux-ci/tekton-catalog/data-acceptable-bundles:latest"
	EcPolicyDataPath    = "github.com/release-engineering/rhtap-ec-policy//data"

	// Service constants
	ApplicationName      string = "application"
	ReferenceDoesntExist string = "Reference does not exist"
)

var ManagednamespaceSecret = []corev1.ObjectReference{
	{Name: RedhatAppstudioUserSecret},
	{Name: ReleaseCatalogTAQuaySecret},
}

// Pipelines variables
var (
	RelSvcCatalogURL                string = utils.GetEnv("RELEASE_SERVICE_CATALOG_URL", "https://github.com/konflux-ci/release-service-catalog")
	RelSvcCatalogRevision           string = utils.GetReleaseServiceCatalogRevision()
	ReleasedImagePushRepo           string = "quay.io/" + utils.GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe") + "/dcmetromap"
	AdditionalReleasedImagePushRepo string = "quay.io/" + utils.GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe") + "/simplepython"
)
