package constants

import "time"

type BuildPipelineType string

// Global constants
const (
	// A github token is required to run the tests. The token need to have permissions to the given github organization. By default the e2e use redhat-appstudio-qe github organization.
	GITHUB_TOKEN_ENV string = "GITHUB_TOKEN" // #nosec

	// The github organization is used to create the gitops repositories in Red Hat Appstudio.
	GITHUB_E2E_ORGANIZATION_ENV string = "MY_GITHUB_ORG" // #nosec

	// The quay organization is used to push container images using Red Hat Appstudio pipelines.
	QUAY_E2E_ORGANIZATION_ENV string = "QUAY_E2E_ORGANIZATION" // #nosec

	// The quay.io username to perform container builds and puush
	QUAY_OAUTH_USER_ENV string = "QUAY_OAUTH_USER" // #nosec

	// A quay organization where repositories for component images will be created.
	DEFAULT_QUAY_ORG_ENV string = "DEFAULT_QUAY_ORG" // #nosec

	// The quay.io token to perform container builds and push. The token must be correlated with the QUAY_OAUTH_USER environment
	QUAY_OAUTH_TOKEN_ENV string = "QUAY_OAUTH_TOKEN" // #nosec

	// The git repo url for the EC pipelines.
	EC_PIPELINES_REPO_URL_ENV string = "EC_PIPELINES_REPO_URL"

	// The repo url for a task. This is used in a git resolver in the tasks package
	TASK_REPO_URL_ENV string = "TASK_REPO_URL"

	// The git repo revision for the EC pipelines.
	EC_PIPELINES_REPO_REVISION_ENV string = "EC_PIPELINES_REPO_REVISION"

	// The task revision to retrieve. This is used in a git resolver in the tasks package
	TASK_REPO_REVISION_ENV string = "TASK_REPO_REVISION"

	// The private devfile sample git repository to use in certain HAS e2e tests
	PRIVATE_DEVFILE_SAMPLE string = "PRIVATE_DEVFILE_SAMPLE" // #nosec

	// The namespace where Tekton Chains and its secrets are deployed.
	TEKTON_CHAINS_NS string = "openshift-pipelines" // #nosec

	// User for running the end-to-end Tekton Chains tests
	TEKTON_CHAINS_E2E_USER string = "chains-e2e"

	// Name of the Secret Tekton Chains uses to read signing key
	TEKTON_CHAINS_SIGNING_SECRETS_NAME = "signing-secrets"

	//Cluster Registration namespace
	CLUSTER_REG_NS string = "cluster-reg-config" // #nosec

	// E2E test namespace where the app and component CRs will be created
	E2E_APPLICATIONS_NAMESPACE_ENV string = "E2E_APPLICATIONS_NAMESPACE"

	// Skip checking "ApplicationServiceGHTokenSecrName" secret
	SKIP_HAS_SECRET_CHECK_ENV string = "SKIP_HAS_SECRET_CHECK"

	// Sandbox kubeconfig user path
	USER_KUBE_CONFIG_PATH_ENV string = "USER_KUBE_CONFIG_PATH"
	// Release e2e auth for build and release quay keys

	QUAY_OAUTH_TOKEN_RELEASE_SOURCE string = "QUAY_OAUTH_TOKEN_RELEASE_SOURCE"

	QUAY_OAUTH_TOKEN_RELEASE_DESTINATION string = "QUAY_OAUTH_TOKEN_RELEASE_DESTINATION"

	// Key auth for accessing Pyxis stage external registry
	PYXIS_STAGE_KEY_ENV string = "PYXIS_STAGE_KEY"

	// Cert auth for accessing Pyxis stage external registry
	PYXIS_STAGE_CERT_ENV string = "PYXIS_STAGE_CERT"

	// SSO user for accessing the Atlas stage release instance
	ATLAS_STAGE_ACCOUNT_ENV string = "ATLAS_STAGE_ACCOUNT" // #nosec

	// SSO token for accessing the Atlas stage release instance
	ATLAS_STAGE_TOKEN_ENV string = "ATLAS_STAGE_TOKEN" // #nosec

	// Atlas AWS account key (stage)
	ATLAS_AWS_ACCESS_KEY_ID_ENV string = "ATLAS_AWS_ACCESS_KEY_ID"

	// Atlas AWS account secret (stage)
	ATLAS_AWS_ACCESS_SECRET_ENV string = "ATLAS_AWS_ACCESS_SECRET"

	// Offline/refresh token used for getting Keycloak token in order to authenticate against stage/prod cluster
	// More details: https://access.redhat.com/articles/3626371
	OFFLINE_TOKEN_ENV = "OFFLINE_TOKEN"

	// Keycloak URL used for authentication against stage/prod cluster
	KEYLOAK_URL_ENV = "KEYLOAK_URL"

	// Toolchain API URL used for authentication against stage/prod cluster
	TOOLCHAIN_API_URL_ENV = "TOOLCHAIN_API_URL"

	// Dev workspace for release pipelines tests
	RELEASE_DEV_WORKSPACE_ENV = "RELEASE_DEV_WORKSPACE"

	// Managed workspace for release pipelines tests
	RELEASE_MANAGED_WORKSPACE_ENV = "RELEASE_MANAGED_WORKSPACE"

	// Bundle ref for a buildah-remote build
	CUSTOM_BUILDAH_REMOTE_PIPELINE_BUILD_BUNDLE_ENV string = "CUSTOM_BUILDAH_REMOTE_PIPELINE_BUILD_BUNDLE"

	// Bundle ref for custom source-build, format example: quay.io/redhat-appstudio-qe/test-images:pipeline-bundle-1715584704-fftb
	CUSTOM_BUILD_PIPELINE_BUNDLE_ENV string = "CUSTOM_BUILD_PIPELINE_BUNDLE"

	// Bundle ref for custom docker-build, format example: quay.io/redhat-appstudio-qe/test-images:pipeline-bundle-1715584704-fftb
	CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE_ENV string = "CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE"

	// Bundle ref for custom docker-build-oci-ta, format example: quay.io/redhat-appstudio-qe/test-images:pipeline-bundle-1715584704-fftb
	CUSTOM_DOCKER_BUILD_OCI_TA_PIPELINE_BUNDLE_ENV string = "CUSTOM_DOCKER_BUILD_OCI_TA_PIPELINE_BUNDLE"

	// Bundle ref for docker-build-oci-ta-min (minimal pipeline). When set, E2E uses this bundle; default from operator manifest.
	CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE_ENV string = "CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE"

	// Bundle ref for custom docker-build-multi-platform-oci-ta, format example: quay.io/redhat-appstudio-qe/test-images:pipeline-bundle-1715584704-fftb
	CUSTOM_DOCKER_BUILD_OCI_MULTI_PLATFORM_TA_PIPELINE_BUNDLE_ENV string = "CUSTOM_DOCKER_BUILD_OCI_MULTI_PLATFORM_TA_PIPELINE_BUNDLE"

	// Bundle ref for custom fbc-builder, format example: quay.io/redhat-appstudio-qe/test-images:pipeline-bundle-1715584704-fftb
	CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE_ENV string = "CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE"

	// QE slack bot token used for delivering messages about critical failures during CI runs
	SLACK_BOT_TOKEN_ENV = "SLACK_BOT_TOKEN"

	// This variable is set by an automation in case Spray Proxy configuration fails in CI
	SKIP_PAC_TESTS_ENV = "SKIP_PAC_TESTS"

	// If set to "true", e2e-tests installer will configure master/control plane nodes as schedulable
	ENABLE_SCHEDULING_ON_MASTER_NODES_ENV = "ENABLE_SCHEDULING_ON_MASTER_NODES"

	// A gitlab bot token is required to run tests against gitlab.com. The token need to have permissions to the Gitlab repository.
	GITLAB_BOT_TOKEN_ENV string = "GITLAB_BOT_TOKEN" // #nosec

	// The GitLab org which owns the test repositories
	GITLAB_QE_ORG_ENV string = "GITLAB_QE_ORG"

	// The gitlab API URL used to run e2e tests against
	GITLAB_API_URL_ENV string = "GITLAB_API_URL" // #nosec

	// GitLab Project ID used for helper functions in magefiles
	GITLAB_PROJECT_ID_ENV string = "GITLAB_PROJECT_ID"

	// A Codeberg bot token is required to run tests against codeberg.org. The token needs to have permissions to the Codeberg organization/repositories.
	CODEBERG_BOT_TOKEN_ENV string = "CODEBERG_BOT_TOKEN" // #nosec

	// The Codeberg org which owns the test repositories
	CODEBERG_QE_ORG_ENV string = "CODEBERG_QE_ORG"

	// The Codeberg base URL used to run e2e tests against (defaults to https://codeberg.org)
	CODEBERG_API_URL_ENV string = "CODEBERG_API_URL" // #nosec

	// The smee.io channel URL for forwarding webhooks to the test cluster
	SMEE_CHANNEL_ENV string = "SMEE_CHANNEL"

	// Release service catalog default URL and revision for e2e tests
	RELEASE_CATALOG_DEFAULT_URL      = "https://github.com/konflux-ci/release-service-catalog.git"
	RELEASE_CATALOG_DEFAULT_REVISION = "staging"

	// We are running tests against 2 types of test environments:
	//
	// * downstream - Konflux deployed from infra-deployments repo, typically on OCP or ROSA
	//
	// * upstream - Konflux deployed from konflux-ci repo, typically running on Kind cluster
	//
	// This env var is meant to be used in the framework to apply a different framework init
	// or a test configuration based on the provided value
	// By default it should use "downstream"
	TEST_ENVIRONMENT_ENV = "TEST_ENVIRONMENT"

	// Test namespace's required labels
	ArgoCDLabelKey   string = "argocd.argoproj.io/managed-by"
	ArgoCDLabelValue string = "gitops-service-argocd"
	// Label for marking a namespace as a tenant namespace
	TenantLabelKey   string = "konflux-ci.dev/type"
	TenantLabelValue string = "tenant"
	// Label for marking a namespace with the workspace label
	WorkspaceLabelKey string = "appstudio.redhat.com/workspace_name"

	BuildPipelinesConfigMapDefaultNamespace = "build-templates"

	HostOperatorNamespace   string = "toolchain-host-operator"
	MemberOperatorNamespace string = "toolchain-member-operator"

	HostOperatorWorkload   string = "host-operator-controller-manager"
	MemberOperatorWorkload string = "member-operator-controller-manager"

	OLMOperatorNamespace string = "openshift-operator-lifecycle-manager"
	OLMOperatorWorkload  string = "olm-operator"

	OSAPIServerNamespace string = "openshift-apiserver"
	OSAPIServerWorkload  string = "apiserver"

	DefaultQuayOrg = "redhat-appstudio-qe"

	DefaultGitLabAPIURL   = "https://gitlab.com/api/v4"
	DefaultGitLabQEOrg    = "konflux-qe"
	DefaultGitLabRepoName = "hacbs-test-project-integration"

	DefaultCodebergAPIURL = "https://codeberg.org"
	DefaultCodebergQEOrg  = "konflux-qe"

	RegistryAuthSecretName = "redhat-appstudio-registry-pull-secret"
	ComponentSecretName    = "comp-secret"

	QuayRepositorySecretName      = "quay-repository"
	QuayRepositorySecretNamespace = "e2e-secrets"

	BuildPipelineConfigConfigMapYamlURL = "https://raw.githubusercontent.com/redhat-appstudio/infra-deployments/main/components/build-service/base/build-pipeline-config/build-pipeline-config.yaml"

	DefaultImagePushRepo         = "quay.io/" + DefaultQuayOrg + "/test-images"
	DefaultReleasedImagePushRepo = "quay.io/" + DefaultQuayOrg + "/test-release-images"

	BuildTaskRunName = "build-container"

	ReleasePipelineImageRef = "quay.io/hacbs-release/pipeline-release:0.20"

	FromIndex   = "registry-proxy.engineering.redhat.com/rh-osbs/iib-preview-rhtap:{{ OCP_VERSION }}"
	TargetIndex = "quay.io/redhat/redhat----preview-operator-index:{{ OCP_VERSION }}"

	StrategyConfigsRepo          = "strategy-configs"
	StrategyConfigsDefaultBranch = "main"
	StrategyConfigsRevision      = "caeaaae63a816ab42dad6c7be1e4b352ea8aabf4"

	TektonTaskTestOutputName = "TEST_OUTPUT"

	DefaultPipelineServiceAccount = "konflux-integration-runner"

	PaCPullRequestBranchPrefix = "konflux-"

	// Expiration for image tags
	IMAGE_TAG_EXPIRATION_ENV  string = "IMAGE_TAG_EXPIRATION"
	DefaultImageTagExpiration string = "6h"

	PipelineRunPollingInterval = 20 * time.Second

	// Reduced from 90 min: if attestation hasn't happened in 20 min, it won't.
	// 90 min held the entire AfterAll block hostage on failure.
	ChainsAttestationTimeout = 20 * time.Minute

	JsonStageUsersPath = "users.json"

	SamplePrivateRepoName = "test-private-repo"

	// Github App name is RHTAP-Qe-App. Note: this App ID is used in our CI and can't be used for local dev/testing.
	DefaultPaCGitHubAppID = "310332"

	// Error string constants for Namespace-backed environment test suite
	SEBAbsenceErrorString          = "no SnapshotEnvironmentBinding found in environment"
	EphemeralEnvAbsenceErrorString = "no matching Ephemeral Environment found"

	// #app-studio-ci-reports channel id
	SlackCIReportsChannelID = "C02M210JZ7B"

	DevReleaseTeam     = "dev-release-team"
	ManagedReleaseTeam = "managed-release-team"

	// Name of the finalizer used for blocking pruning of E2E test PipelineRuns
	E2ETestFinalizerName = "e2e-test"

	// Default github repo values for build
	DEFAULT_GITHUB_BUILD_ORG  = "redhat-appstudio"
	DEFAULT_GITHUB_BUILD_REPO = "build-definitions"

	PaCControllerNamespace = "openshift-pipelines"
	PaCControllerRouteName = "pipelines-as-code-controller"

	DockerFilePath = "docker/Dockerfile"

	CheckrunConclusionSuccess = "success"
	CheckrunConclusionFailure = "failure"
	CheckrunStatusCompleted   = "completed"
	CheckrunConclusionNeutral = "neutral"

	DockerBuild                   BuildPipelineType = "docker-build"
	DockerBuildOciTA              BuildPipelineType = "docker-build-oci-ta"
	DockerBuildOciTAMin           BuildPipelineType = "docker-build-oci-ta-min"
	DockerBuildMultiPlatformOciTa BuildPipelineType = "docker-build-multi-platform-oci-ta"
	FbcBuilder                    BuildPipelineType = "fbc-builder"

	// Test environments
	DownstreamTestEnvironment string = "downstream"
	UpstreamTestEnvironment   string = "upstream"

	// A cluster role used to be bound to a user that has admin access to all Konflux resources in a specific namespace
	// https://github.com/konflux-ci/konflux-ci/blob/2772e3b648ce1c1ae05f31e77732063c4103de09/konflux-ci/rbac/core/konflux-admin-user-actions.yaml
	KonfluxAdminUserActionsClusterRoleName = "konflux-admin-user-actions"
	// Default role binding name
	DefaultKonfluxAdminRoleBindingName = "user2-konflux-admin"
	// Default user name available after deploying upstream version of konflux-ci
	DefaultKonfluxCIUserName = "user2@konflux.dev"

	DefaultGilabGroupId = "85150202"
)

var (
	ComponentPaCRequestAnnotation               = map[string]string{"build.appstudio.openshift.io/request": "configure-pac"}
	ComponentTriggerSimpleBuildAnnotation       = map[string]string{"build.appstudio.openshift.io/request": "trigger-simple-build"}
	ImageControllerAnnotationRequestPublicRepo  = map[string]string{"image.redhat.com/generate": `{"visibility": "public"}`}
	ImageControllerAnnotationRequestPrivateRepo = map[string]string{"image.redhat.com/generate": `{"visibility": "private"}`}
	IntegrationTestScenarioDefaultLabels        = map[string]string{"test.appstudio.openshift.io/optional": "false"}
	DefaultDockerBuildPipelineBundleAnnotation  = map[string]string{"build.appstudio.openshift.io/pipeline": `{"name": "docker-build", "bundle": "latest"}`}
	DefaultFbcBuilderPipelineBundle             = map[string]string{"build.appstudio.openshift.io/pipeline": `{"name": "fbc-builder", "bundle": "latest"}`}
	ComponentMintmakerDisabledAnnotation        = map[string]string{"mintmaker.appstudio.redhat.com/disabled": "true"}
	GitLabProjectIdsMap                         = map[string]string{"hacbs-test-project-integration": "56586709", "devfile-sample-hello-world": "60038001", "build-nudge-parent": "62134305", "build-nudge-child": "62134341"}
)

func GetGitLabProjectId(repoName string) string {
	for name, id := range GitLabProjectIdsMap {
		if name == repoName {
			return id
		}
	}
	return ""
}
