package constants

import "time"

type BuildPipelineType string

const (
	GITHUB_TOKEN_ENV            string = "GITHUB_TOKEN"            // #nosec
	GITHUB_E2E_ORGANIZATION_ENV string = "MY_GITHUB_ORG"           // #nosec
	QUAY_E2E_ORGANIZATION_ENV   string = "QUAY_E2E_ORGANIZATION"   // #nosec

	TEKTON_CHAINS_NS string = "openshift-pipelines" // #nosec

	E2E_APPLICATIONS_NAMESPACE_ENV string = "E2E_APPLICATIONS_NAMESPACE"

	SKIP_PAC_TESTS_ENV = "SKIP_PAC_TESTS"

	GITLAB_BOT_TOKEN_ENV string = "GITLAB_BOT_TOKEN" // #nosec
	GITLAB_QE_ORG_ENV    string = "GITLAB_QE_ORG"
	GITLAB_API_URL_ENV   string = "GITLAB_API_URL" // #nosec


	// Custom pipeline bundle overrides
	CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE_ENV                      string = "CUSTOM_DOCKER_BUILD_PIPELINE_BUNDLE"
	CUSTOM_DOCKER_BUILD_OCI_TA_PIPELINE_BUNDLE_ENV               string = "CUSTOM_DOCKER_BUILD_OCI_TA_PIPELINE_BUNDLE"
	CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE_ENV           string = "CUSTOM_DOCKER_BUILD_OCI_TA_MIN_PIPELINE_BUNDLE"
	CUSTOM_DOCKER_BUILD_OCI_MULTI_PLATFORM_TA_PIPELINE_BUNDLE_ENV string = "CUSTOM_DOCKER_BUILD_OCI_MULTI_PLATFORM_TA_PIPELINE_BUNDLE"
	CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE_ENV                       string = "CUSTOM_FBC_BUILDER_PIPELINE_BUNDLE"

	TEST_ENVIRONMENT_ENV = "TEST_ENVIRONMENT"

	// Namespace labels
	ArgoCDLabelKey   string = "argocd.argoproj.io/managed-by"
	ArgoCDLabelValue string = "gitops-service-argocd"
	TenantLabelKey   string = "konflux-ci.dev/type"
	TenantLabelValue string = "tenant"
	WorkspaceLabelKey string = "appstudio.redhat.com/workspace_name"

	DefaultGitLabAPIURL   = "https://gitlab.com/api/v4"
	DefaultGitLabQEOrg    = "konflux-qe"

	RegistryAuthSecretName = "redhat-appstudio-registry-pull-secret"
	ComponentSecretName    = "comp-secret"

	QuayRepositorySecretName      = "quay-repository"
	QuayRepositorySecretNamespace = "e2e-secrets"

	BuildPipelineConfigConfigMapYamlURL = "https://raw.githubusercontent.com/redhat-appstudio/infra-deployments/main/components/build-service/base/build-pipeline-config/build-pipeline-config.yaml"

	TektonTaskTestOutputName     = "TEST_OUTPUT"
	DefaultPipelineServiceAccount = "konflux-integration-runner"
	PaCPullRequestBranchPrefix    = "konflux-"

	PipelineRunPollingInterval = 20 * time.Second

	SamplePrivateRepoName = "test-private-repo"
	DefaultPaCGitHubAppID = "310332"

	CheckrunStatusCompleted = "completed"

	E2ETestFinalizerName = "e2e-test"

	DEFAULT_GITHUB_BUILD_ORG  = "redhat-appstudio"
	DEFAULT_GITHUB_BUILD_REPO = "build-definitions"

	// Build pipeline types
	DockerBuild                   BuildPipelineType = "docker-build"
	DockerBuildOciTA              BuildPipelineType = "docker-build-oci-ta"
	DockerBuildOciTAMin           BuildPipelineType = "docker-build-oci-ta-min"
	DockerBuildMultiPlatformOciTa BuildPipelineType = "docker-build-multi-platform-oci-ta"
	FbcBuilder                    BuildPipelineType = "fbc-builder"

	UpstreamTestEnvironment string = "upstream"

	// RBAC
	KonfluxAdminUserActionsClusterRoleName = "konflux-admin-user-actions"
	DefaultKonfluxAdminRoleBindingName     = "user2-konflux-admin"
	DefaultKonfluxCIUserName               = "user2@konflux.dev"

	DefaultGilabGroupId = "85150202"
)

var (
	ComponentPaCRequestAnnotation              = map[string]string{"build.appstudio.openshift.io/request": "configure-pac"}
	ComponentTriggerSimpleBuildAnnotation       = map[string]string{"build.appstudio.openshift.io/request": "trigger-simple-build"}
	ImageControllerAnnotationRequestPublicRepo = map[string]string{"image.redhat.com/generate": `{"visibility": "public"}`}
	IntegrationTestScenarioDefaultLabels       = map[string]string{"test.appstudio.openshift.io/optional": "false"}
	GitLabProjectIdsMap                        = map[string]string{"hacbs-test-project-integration": "56586709", "devfile-sample-hello-world": "60038001", "build-nudge-parent": "62134305", "build-nudge-child": "62134341"}
)

func GetGitLabProjectId(repoName string) string {
	for name, id := range GitLabProjectIdsMap {
		if name == repoName {
			return id
		}
	}
	return ""
}
