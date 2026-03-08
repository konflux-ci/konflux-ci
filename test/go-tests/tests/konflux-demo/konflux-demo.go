package konflux_demo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	buildcontrollers "github.com/konflux-ci/build-service/controllers"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"

	k8sErrors "k8s.io/apimachinery/pkg/api/errors"

	ecp "github.com/conforma/crds/api/v1alpha1"
	"github.com/devfile/library/v2/pkg/util"
	"github.com/google/go-github/v44/github"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/has"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/framework"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/build"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/tekton"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	integrationv1beta2 "github.com/konflux-ci/integration-service/api/v1beta2"
	releasecommon "github.com/konflux-ci/konflux-ci/test/go-tests/tests/release"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonapi "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	kubeapi "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kubernetes"
	e2eConfig "github.com/konflux-ci/konflux-ci/test/go-tests/tests/konflux-demo/config"
	"k8s.io/klog/v2"
)

// Set QUAY_TOKEN from QUAY_DOCKERCONFIGJSON if only the latter is set (e.g. when sourcing e2e.env).
// Must be at top level for Ginkgo.
var _ = ginkgo.BeforeSuite(func() {
	fmt.Println("Starting Konflux e2e tests...")
	if os.Getenv("QUAY_TOKEN") == "" {
		if qdc := os.Getenv("QUAY_DOCKERCONFIGJSON"); qdc != "" {
			os.Setenv("QUAY_TOKEN", base64.StdEncoding.EncodeToString([]byte(qdc)))
		}
	}
})

var _ = framework.KonfluxDemoSuiteDescribe(ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
	defer ginkgo.GinkgoRecover()

	var timeout, interval time.Duration
	var userNamespace string
	var err error

	var managedNamespace string

	var component *appservice.Component
	var release *releaseApi.Release
	var snapshot *appservice.Snapshot
	var pipelineRun, testPipelinerun *tektonapi.PipelineRun
	var integrationTestScenario *integrationv1beta2.IntegrationTestScenario

	// PaC related variables
	var prNumber int
	var headSHA, pacBranchName string
	var mergeResult *github.PullRequestMergeResult

	//secret := &corev1.Secret{}

	fw := &framework.Framework{}

	var buildPipelineAnnotation map[string]string

	var componentNewBaseBranch, gitRevision, componentRepositoryName, componentName string

	appSpecs := e2eConfig.UpstreamAppSpecs

	for _, appSpec := range appSpecs {
		appSpec := appSpec
		if appSpec.Skip {
			continue
		}

		ginkgo.Describe(appSpec.Name, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func() {
				if os.Getenv(constants.SKIP_PAC_TESTS_ENV) == "true" {
					ginkgo.Skip("Skipping this test due to configuration issue with Spray proxy")
				}
				klog.Info("Konflux demo BeforeAll: initializing framework for app", "app", appSpec.Name)
				fw, err = framework.NewFramework(utils.GetGeneratedNamespace(devEnvTestLabel))
				if err != nil {
					klog.Errorf("Konflux demo BeforeAll: failed to create framework: %v", err)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}
				userNamespace = fw.UserNamespace
				managedNamespace = userNamespace + "-managed"
				klog.Info("Konflux demo BeforeAll: namespaces", "userNamespace", userNamespace, "managedNamespace", managedNamespace)

				// Component config
				componentName = fmt.Sprintf("%s-%s", appSpec.ComponentSpec.Name, util.GenerateRandomString(4))
				pacBranchName = fmt.Sprintf("%s%s", constants.PaCPullRequestBranchPrefix, componentName)
				componentRepositoryName = utils.ExtractGitRepositoryNameFromURL(appSpec.ComponentSpec.GitSourceUrl)
				klog.Info("Konflux demo BeforeAll: component config", "componentName", componentName, "pacBranchName", pacBranchName, "componentRepositoryName", componentRepositoryName)

				// Secrets config
				// https://issues.redhat.com/browse/KFLUXBUGS-1462 - creating SCM secret alongside with PaC
				// leads to PLRs being duplicated
				// secretDefinition := build.GetSecretDefForGitHub(namespace)
				// secret, err = fw.AsKubeAdmin.CommonController.CreateSecret(namespace, secretDefinition)
				sharedSecret, err := fw.AsKubeAdmin.CommonController.GetSecret(constants.QuayRepositorySecretNamespace, constants.QuayRepositorySecretName)
				if err != nil && k8sErrors.IsNotFound(err) {
					klog.Info("Konflux demo BeforeAll: Quay secret not found, creating E2E Quay secret")
					sharedSecret, err = CreateE2EQuaySecret(fw.AsKubeAdmin.CommonController.CustomClient)
					if err != nil {
						klog.Errorf("Konflux demo BeforeAll: failed to create E2E Quay secret: %v", err)
						gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					}
				}
				if err != nil {
					klog.Errorf("Konflux demo BeforeAll: failed to get shared secret %s in %s: %v", constants.QuayRepositorySecretName, constants.QuayRepositorySecretNamespace, err)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), fmt.Sprintf("error when getting shared secret - make sure the secret %s in %s userNamespace is created", constants.QuayRepositorySecretName, constants.QuayRepositorySecretNamespace))
				}

				klog.Info("Konflux demo BeforeAll: creating release config", "managedNamespace", managedNamespace, "userNamespace", userNamespace)
				createReleaseConfig(fw.AsKubeAdmin, managedNamespace, userNamespace, appSpec.ComponentSpec.Name, appSpec.ApplicationName, sharedSecret.Data[".dockerconfigjson"], utils.GetEnv("RELEASE_TA_OCI_STORAGE", ""))

				// When RELEASE_CATALOG_TA_QUAY_TOKEN is set, create and link the TA Quay secret so the release
				// pipeline can push to quay.io/konflux-ci/release-service-trusted-artifacts (same as happy_path
				// and push_to_external_registry). Aligns with openshift/release and infra-deployments e2e flow.
				taToken := utils.GetEnv("RELEASE_CATALOG_TA_QUAY_TOKEN", "")
				if taToken != "" {
					_, err = fw.AsKubeAdmin.CommonController.CreateRegistryAuthSecret(releasecommon.ReleaseCatalogTAQuaySecret, managedNamespace, taToken)
					if err != nil {
						klog.Errorf("Konflux demo BeforeAll: failed to create TA Quay secret in %s: %v", managedNamespace, err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					err = fw.AsKubeAdmin.CommonController.LinkSecretToServiceAccount(managedNamespace, releasecommon.ReleaseCatalogTAQuaySecret, "release-service-account", true)
					if err != nil {
						klog.Errorf("Konflux demo BeforeAll: failed to link TA Quay secret to release-service-account in %s: %v", managedNamespace, err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					klog.Info("Konflux demo BeforeAll: created and linked release-catalog-trusted-artifacts-quay-secret", "managedNamespace", managedNamespace)
					ginkgo.GinkgoWriter.Printf("created and linked release-catalog-trusted-artifacts-quay-secret in namespace %q\n", managedNamespace)
				} else {
					klog.Info("Konflux demo BeforeAll: RELEASE_CATALOG_TA_QUAY_TOKEN not set, skipping TA Quay secret")
					ginkgo.GinkgoWriter.Printf("RELEASE_CATALOG_TA_QUAY_TOKEN not set, skipping TA Quay secret (release pipeline may fail on trusted-artifact push)\n")
				}

				// get the build pipeline bundle annotation
				buildPipelineAnnotation = build.GetBuildPipelineBundleAnnotation(appSpec.ComponentSpec.BuildPipelineType)
				klog.Info("Konflux demo BeforeAll: setup complete", "app", appSpec.Name, "componentName", componentName)
			})

			// Remove all resources created by the tests.
			// Cleanup is best-effort: do not fail the test for namespace, ref, or webhook cleanup errors.
			ginkgo.AfterAll(func() {
				if !(strings.EqualFold(os.Getenv("E2E_SKIP_CLEANUP"), "true")) && !ginkgo.CurrentSpecReport().Failed() && !strings.Contains(ginkgo.GinkgoLabelFilter(), upstreamKonfluxTestLabel) {
					klog.Info("Konflux demo AfterAll: cleaning up namespaces", "userNamespace", userNamespace, "managedNamespace", managedNamespace)
					if err = fw.AsKubeAdmin.CommonController.DeleteNamespace(userNamespace); err != nil {
						klog.Errorf("Konflux demo AfterAll: failed to delete user namespace %s: %v", userNamespace, err)
					}
					if err = fw.AsKubeAdmin.CommonController.DeleteNamespace(managedNamespace); err != nil {
						klog.Errorf("Konflux demo AfterAll: failed to delete managed namespace %s: %v", managedNamespace, err)
					}

					// Delete new branch created by PaC and a testing branch used as a component's base branch.
					// Best-effort: token may lack ref write or repo hooks scope (e.g. upstream/local); do not fail the test.
					if err = fw.AsKubeAdmin.CommonController.Github.DeleteRef(componentRepositoryName, pacBranchName); err != nil {
						klog.Errorf("Konflux demo AfterAll: failed to delete GitHub ref %s in repo %s: %v", pacBranchName, componentRepositoryName, err)
					}
					if err = fw.AsKubeAdmin.CommonController.Github.DeleteRef(componentRepositoryName, componentNewBaseBranch); err != nil {
						klog.Errorf("Konflux demo AfterAll: failed to delete GitHub ref %s in repo %s: %v", componentNewBaseBranch, componentRepositoryName, err)
					}
					if err = build.CleanupWebhooks(fw, componentRepositoryName); err != nil {
						klog.Errorf("Konflux demo AfterAll: failed to cleanup webhooks for repo %s: %v", componentRepositoryName, err)
					}
					klog.Info("Konflux demo AfterAll: cleanup finished", "componentRepositoryName", componentRepositoryName)
				}
			})

			// Create an application in a specific namespace
			ginkgo.It("creates an application", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				klog.Info("Konflux demo: creating application", "applicationName", appSpec.ApplicationName, "namespace", userNamespace)
				createdApplication, err := fw.AsKubeAdmin.HasController.CreateApplication(appSpec.ApplicationName, userNamespace)
				if err != nil {
					klog.Errorf("Konflux demo: failed to create application %s in %s: %v", appSpec.ApplicationName, userNamespace, err)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}
				gomega.Expect(createdApplication.Spec.DisplayName).To(gomega.Equal(appSpec.ApplicationName))
				gomega.Expect(createdApplication.Namespace).To(gomega.Equal(userNamespace))
				klog.Info("Konflux demo: application created", "applicationName", appSpec.ApplicationName)
			})

			// Create an IntegrationTestScenario for the App
			ginkgo.It("creates an IntegrationTestScenario for the app", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				klog.Info("Konflux demo: creating IntegrationTestScenario", "applicationName", appSpec.ApplicationName, "namespace", userNamespace)
				its := appSpec.ComponentSpec.IntegrationTestScenario
				// Use Eventually to handle race condition where admission webhook hasn't indexed the application yet
				gomega.Eventually(func() error {
					var err error
					integrationTestScenario, err = fw.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario("", appSpec.ApplicationName, userNamespace, its.GitURL, its.GitRevision, its.TestPath, "", []string{})
					if err != nil {
						klog.Errorf("Konflux demo: CreateIntegrationTestScenario failed (will retry): %v", err)
						return err
					}
					return nil
				}, time.Minute*2, time.Second*5).Should(gomega.Succeed(), "timed out creating IntegrationTestScenario for app %s in %s", appSpec.ApplicationName, userNamespace)
				klog.Info("Konflux demo: IntegrationTestScenario created", "scenario", integrationTestScenario.Name)
			})

			ginkgo.It("creates new branch for the build", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				// We need to create a new branch that we will target
				// and that will contain the PaC configuration, so we
				// can avoid polluting the default (main) branch
				componentNewBaseBranch = fmt.Sprintf("base-%s", util.GenerateRandomString(6))
				gitRevision = componentNewBaseBranch
				klog.Info("Konflux demo: creating branch for build", "repo", componentRepositoryName, "branch", componentNewBaseBranch, "from", appSpec.ComponentSpec.GitSourceDefaultBranchName)
				err = fw.AsKubeAdmin.CommonController.Github.CreateRef(componentRepositoryName, appSpec.ComponentSpec.GitSourceDefaultBranchName, appSpec.ComponentSpec.GitSourceRevision, componentNewBaseBranch)
				if err != nil {
					klog.Errorf("Konflux demo: failed to create ref %s in repo %s: %v", componentNewBaseBranch, componentRepositoryName, err)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				}
			})

			// Component are imported from gitUrl
			ginkgo.It(fmt.Sprintf("creates component %s (private: %t) from git source %s", appSpec.ComponentSpec.Name, appSpec.ComponentSpec.Private, appSpec.ComponentSpec.GitSourceUrl), ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				klog.Info("Konflux demo: creating component", "componentName", componentName, "applicationName", appSpec.ApplicationName, "namespace", userNamespace, "revision", gitRevision)
				componentObj := appservice.ComponentSpec{
					ComponentName: componentName,
					Application:   appSpec.ApplicationName,
					Source: appservice.ComponentSource{
						ComponentSourceUnion: appservice.ComponentSourceUnion{
							GitSource: &appservice.GitSource{
								URL:           appSpec.ComponentSpec.GitSourceUrl,
								Revision:      gitRevision,
								Context:       appSpec.ComponentSpec.GitSourceContext,
								DockerfileURL: appSpec.ComponentSpec.DockerFilePath,
							},
						},
					},
				}

				component, err = fw.AsKubeAdmin.HasController.CreateComponentCheckImageRepository(componentObj, userNamespace, "", "", appSpec.ApplicationName, false, utils.MergeMaps(constants.ComponentPaCRequestAnnotation, buildPipelineAnnotation))
				if err != nil {
					klog.Errorf("Konflux demo: failed to create component %s in %s: %v", componentName, userNamespace, err)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
				}
				klog.Info("Konflux demo: component created", "componentName", component.GetName())
			})

			ginkgo.When("Component is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("triggers creation of a PR in the sample repo", func() {
					var prSHA string
					klog.Info("Konflux demo: waiting for PaC PR", "repo", componentRepositoryName, "pacBranchName", pacBranchName)
					gomega.Eventually(func() error {
						prs, err := fw.AsKubeAdmin.CommonController.Github.ListPullRequests(componentRepositoryName)
						if err != nil {
							klog.Errorf("Konflux demo: ListPullRequests failed for %s: %v", componentRepositoryName, err)
							return err
						}
						for _, pr := range prs {
							if pr.Head.GetRef() == pacBranchName {
								prNumber = pr.GetNumber()
								prSHA = pr.GetHead().GetSHA()
								return nil
							}
						}
						err = fmt.Errorf("could not get the expected PaC branch name %s", pacBranchName)
						klog.Errorf("Konflux demo: %v (found %d PRs)", err, len(prs))
						// Best-effort diagnostics: Component, Application, PaC Repository, PipelineRuns
						if c, getErr := fw.AsKubeAdmin.HasController.GetComponent(component.GetName(), userNamespace); getErr != nil {
							klog.Errorf("Konflux demo: (diagnostic) could not re-fetch Component %s/%s: %v", userNamespace, component.GetName(), getErr)
						} else {
							msgs, _ := fw.AsKubeAdmin.HasController.GetComponentConditionStatusMessages(c.GetName(), userNamespace)
							buildStatusAnnot := c.Annotations[buildcontrollers.BuildStatusAnnotationName]
							klog.Infof("Konflux demo: (diagnostic) Component %s/%s status conditions: %v; build status annotation: %q", userNamespace, c.GetName(), msgs, buildStatusAnnot)
						}
						if app, getErr := fw.AsKubeAdmin.HasController.GetApplication(appSpec.ApplicationName, userNamespace); getErr != nil {
							klog.Errorf("Konflux demo: (diagnostic) could not get Application %s/%s: %v", userNamespace, appSpec.ApplicationName, getErr)
						} else if len(app.Status.Conditions) > 0 {
							klog.Infof("Konflux demo: (diagnostic) Application %s/%s status conditions: %+v", userNamespace, app.Name, app.Status.Conditions)
						} else {
							klog.Infof("Konflux demo: (diagnostic) Application %s/%s has no status conditions", userNamespace, app.Name)
						}
						if params, repoErr := fw.AsKubeAdmin.TektonController.GetRepositoryParams(component.GetName(), userNamespace); repoErr != nil {
							klog.Errorf("Konflux demo: (diagnostic) PaC Repository for component %s: %v", component.GetName(), repoErr)
						} else {
							klog.Infof("Konflux demo: (diagnostic) PaC Repository exists for component %s (params count: %d)", component.GetName(), len(params))
						}
						if prList, listErr := fw.AsKubeAdmin.TektonController.ListAllPipelineRuns(userNamespace); listErr != nil {
							klog.Errorf("Konflux demo: (diagnostic) could not list PipelineRuns in %s: %v", userNamespace, listErr)
						} else {
							klog.Infof("Konflux demo: (diagnostic) PipelineRuns in %s: %d", userNamespace, len(prList.Items))
						}
						return err
					}, pullRequestCreationTimeout, defaultPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("timed out when waiting for init PaC PR (branch %q) to be created against the %q repo", pacBranchName, componentRepositoryName))
					klog.Info("Konflux demo: PaC PR created", "prNumber", prNumber, "prSHA", prSHA)

					// We don't need the PipelineRun from a PaC 'pull-request' event to finish, so we can delete it
					klog.Info("Konflux demo: waiting for pull-request PipelineRun to appear (will delete it)", "component", component.GetName(), "prSHA", prSHA)
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, prSHA)
						if err != nil {
							ginkgo.GinkgoWriter.Printf("PipelineRun not found yet for component %s/%s prSHA %s: %v\n", userNamespace, component.GetName(), prSHA, err)
							return err
						}
						if err = fw.AsKubeAdmin.TektonController.DeletePipelineRun(pipelineRun.Name, pipelineRun.Namespace); err != nil {
							klog.Errorf("Konflux demo: failed to delete pull-request PipelineRun %s/%s: %v", pipelineRun.Namespace, pipelineRun.Name, err)
							return err
						}
						return nil
					}, pipelineRunStartedTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("timed out when waiting for `pull-request` event type PaC PipelineRun to be present in the user namespace %q for component %q with a label pointing to %q", userNamespace, component.GetName(), appSpec.ApplicationName))
				})

				ginkgo.It("verifies component build status", func() {
					var buildStatus *buildcontrollers.BuildStatus
					timeout = time.Minute * 5
					interval = defaultPollingInterval
					klog.Info("Konflux demo: verifying component build status (PaC enabled)", "component", component.GetName(), "namespace", userNamespace)
					gomega.Eventually(func() (bool, error) {
						component, err := fw.AsKubeAdmin.HasController.GetComponent(component.GetName(), userNamespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetComponent %s failed: %v", component.GetName(), err)
							return false, err
						}

						statusBytes := []byte(component.Annotations[buildcontrollers.BuildStatusAnnotationName])

						err = json.Unmarshal(statusBytes, &buildStatus)
						if err != nil {
							klog.Errorf("Konflux demo: failed to unmarshal build status for %s: %v", component.GetName(), err)
							return false, err
						}

						if buildStatus.PaC != nil {
							ginkgo.GinkgoWriter.Printf("state: %s\n", buildStatus.PaC.State)
							ginkgo.GinkgoWriter.Printf("mergeUrl: %s\n", buildStatus.PaC.MergeUrl)
							ginkgo.GinkgoWriter.Printf("errId: %d\n", buildStatus.PaC.ErrId)
							ginkgo.GinkgoWriter.Printf("errMessage: %s\n", buildStatus.PaC.ErrMessage)
							ginkgo.GinkgoWriter.Printf("configurationTime: %s\n", buildStatus.PaC.ConfigurationTime)
							if buildStatus.PaC.ErrId != 0 {
								klog.Errorf("Konflux demo: PaC build status has errId=%d errMessage=%s", buildStatus.PaC.ErrId, buildStatus.PaC.ErrMessage)
							}
						} else {
							ginkgo.GinkgoWriter.Println("build status does not have PaC field")
							klog.Errorf("Konflux demo: component %s build status has no PaC field", component.GetName())
						}

						return buildStatus.PaC != nil && buildStatus.PaC.State == "enabled" && buildStatus.PaC.MergeUrl != "" && buildStatus.PaC.ErrId == 0 && buildStatus.PaC.ConfigurationTime != "", nil
					}, timeout, interval).Should(gomega.BeTrue(), "component build status has unexpected content (check PaC state, mergeUrl, errId, errMessage)")
				})

				ginkgo.It("should eventually lead to triggering a 'push' event type PipelineRun after merging the PaC init branch ", func() {
					klog.Info("Konflux demo: merging PaC PR", "repo", componentRepositoryName, "prNumber", prNumber)
					gomega.Eventually(func() error {
						mergeResult, err = fw.AsKubeAdmin.CommonController.Github.MergePullRequest(componentRepositoryName, prNumber)
						if err != nil {
							klog.Errorf("Konflux demo: MergePullRequest failed for PR #%d: %v", prNumber, err)
							return err
						}
						return nil
					}, mergePRTimeout).ShouldNot(gomega.HaveOccurred(), fmt.Sprintf("error when merging PaC pull request: %+v\n", err))

					headSHA = mergeResult.GetSHA()
					klog.Info("Konflux demo: PaC PR merged", "headSHA", headSHA)

					klog.Info("Konflux demo: waiting for push PipelineRun to start", "component", component.GetName(), "headSHA", headSHA)
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
						if err != nil {
							ginkgo.GinkgoWriter.Printf("PipelineRun has not been created yet for component %s/%s\n", userNamespace, component.GetName())
							klog.Errorf("Konflux demo: GetComponentPipelineRun failed: %v", err)
							return err
						}
						if !pipelineRun.HasStarted() {
							err = fmt.Errorf("pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
							ginkgo.GinkgoWriter.Printf("PipelineRun %s/%s not started yet\n", pipelineRun.GetNamespace(), pipelineRun.GetName())
							return err
						}
						return nil
					}, pipelineRunStartedTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("timed out when waiting for a PipelineRun in namespace %q with label component label %q and application label %q and sha label %q to start", userNamespace, component.GetName(), appSpec.ApplicationName, headSHA))
					klog.Info("Konflux demo: push PipelineRun started", "pipelineRun", pipelineRun.Name)
				})
			})

			ginkgo.When("Build PipelineRun is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("does not contain an annotation with a Snapshot Name", func() {
					klog.Info("Konflux demo: checking build PipelineRun has no snapshot annotation", "pipelineRun", pipelineRun.Name)
					gomega.Expect(pipelineRun.Annotations["appstudio.openshift.io/snapshot"]).To(gomega.Equal(""))
				})
				ginkgo.It("should eventually complete successfully", func() {
					klog.Info("Konflux demo: waiting for build PipelineRun to finish", "pipelineRun", pipelineRun.Name, "component", component.GetName(), "headSHA", headSHA)
					err = fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "build", headSHA, "",
						fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 5, Always: true}, pipelineRun)
					if err != nil {
						klog.Errorf("Konflux demo: WaitForComponentPipelineToBeFinished failed for %s: %v", pipelineRun.Name, err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					// in case the first pipelineRun attempt has failed and was retried, we need to update the git branch head ref
					headSHA = pipelineRun.Labels["pipelinesascode.tekton.dev/sha"]
					klog.Info("Konflux demo: build PipelineRun finished", "pipelineRun", pipelineRun.Name, "headSHA", headSHA)
				})
			})

			ginkgo.When("Build PipelineRun completes successfully", func() {

				ginkgo.It("should validate Tekton TaskRun test results successfully", func() {
					klog.Info("Konflux demo: validating build PipelineRun TaskRun test results", "component", component.GetName(), "headSHA", headSHA)
					pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
					if err != nil {
						klog.Errorf("Konflux demo: GetComponentPipelineRun failed: %v", err)
						gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					}
					err = build.ValidateBuildPipelineTestResults(pipelineRun, fw.AsKubeAdmin.CommonController.KubeRest(), false)
					if err != nil {
						klog.Errorf("Konflux demo: ValidateBuildPipelineTestResults failed for %s: %v", pipelineRun.Name, err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
				})

				ginkgo.It("should validate that the build pipelineRun is signed", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					klog.Info("Konflux demo: validating build PipelineRun is signed", "component", component.GetName())
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
						if err != nil {
							klog.Errorf("Konflux demo: GetComponentPipelineRun failed (signed check): %v", err)
							return err
						}
						if pipelineRun.Annotations["chains.tekton.dev/signed"] != "true" {
							return fmt.Errorf("pipelinerun %s/%s does not have the expected value of annotation 'chains.tekton.dev/signed'", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, time.Minute*5, time.Second*5).Should(gomega.Succeed(), "failed while validating build pipelineRun is signed")
					klog.Info("Konflux demo: build PipelineRun is signed", "pipelineRun", pipelineRun.Name)
				})

				ginkgo.It("should find the related Snapshot CR", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					klog.Info("Konflux demo: waiting for Snapshot CR", "pipelineRun", pipelineRun.Name, "namespace", userNamespace)
					gomega.Eventually(func() error {
						snapshot, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", pipelineRun.Name, "", userNamespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetSnapshot for PipelineRun %s failed: %v", pipelineRun.Name, err)
							return err
						}
						return nil
					}, snapshotTimeout, snapshotPollingInterval).Should(gomega.Succeed(), "timed out when trying to check if the Snapshot exists for PipelineRun %s/%s", userNamespace, pipelineRun.GetName())
					klog.Info("Konflux demo: Snapshot found", "snapshot", snapshot.Name)
				})

				ginkgo.It("should validate that the build pipelineRun is annotated with the name of the Snapshot", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
					if err != nil {
						klog.Errorf("Konflux demo: GetComponentPipelineRun failed: %v", err)
						gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					}
					gomega.Expect(pipelineRun.Annotations["appstudio.openshift.io/snapshot"]).To(gomega.Equal(snapshot.GetName()))
				})

				ginkgo.It("should find the related Integration Test PipelineRun", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					klog.Info("Konflux demo: waiting for Integration Test PipelineRun", "scenario", integrationTestScenario.Name, "snapshot", snapshot.Name)
					gomega.Eventually(func() error {
						testPipelinerun, err = fw.AsKubeAdmin.IntegrationController.GetIntegrationPipelineRun(integrationTestScenario.Name, snapshot.Name, userNamespace)
						if err != nil {
							ginkgo.GinkgoWriter.Printf("failed to get Integration test PipelineRun for a snapshot '%s' in '%s' namespace: %+v\n", snapshot.Name, userNamespace, err)
							klog.Errorf("Konflux demo: GetIntegrationPipelineRun failed: %v", err)
							return err
						}
						if !testPipelinerun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", testPipelinerun.GetNamespace(), testPipelinerun.GetName())
						}
						return nil
					}, pipelineRunStartedTimeout, defaultPollingInterval).Should(gomega.Succeed())
					gomega.Expect(testPipelinerun.Labels["appstudio.openshift.io/snapshot"]).To(gomega.ContainSubstring(snapshot.Name))
					gomega.Expect(testPipelinerun.Labels["test.appstudio.openshift.io/scenario"]).To(gomega.ContainSubstring(integrationTestScenario.Name))
					klog.Info("Konflux demo: Integration Test PipelineRun found", "pipelineRun", testPipelinerun.Name)
				})
			})

			ginkgo.When("push pipelinerun is retriggered", func() {
				ginkgo.It("should eventually succeed", func() {
					klog.Info("Konflux demo: triggering PaC build retrigger", "component", component.GetName())
					err = fw.AsKubeAdmin.HasController.SetComponentAnnotation(component.GetName(), buildcontrollers.BuildRequestAnnotationName, buildcontrollers.BuildRequestTriggerPaCBuildAnnotationValue, userNamespace)
					if err != nil {
						klog.Errorf("Konflux demo: SetComponentAnnotation (trigger PaC build) failed: %v", err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					klog.Info("Konflux demo: waiting for retriggered PipelineRun to appear", "component", component.GetName())
					gomega.Eventually(func() error {
						testPipelinerun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRunWithType(component.GetName(), appSpec.ApplicationName, userNamespace, "build", "", "incoming")
						if err != nil {
							ginkgo.GinkgoWriter.Printf("PipelineRun is not been retriggered yet for the component %s/%s\n", userNamespace, component.GetName())
							klog.Errorf("Konflux demo: GetComponentPipelineRunWithType (incoming) failed: %v", err)
							return err
						}
						if !testPipelinerun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't been started yet", testPipelinerun.GetNamespace(), testPipelinerun.GetName())
						}
						return nil
					}, 10*time.Minute, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("timed out when waiting for the PipelineRun to retrigger for the component %s/%s", userNamespace, component.GetName()))
					klog.Info("Konflux demo: waiting for retriggered build PipelineRun to finish", "pipelineRun", testPipelinerun.Name)
					err = fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "build", "", "incoming", fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, testPipelinerun)
					if err != nil {
						klog.Errorf("Konflux demo: WaitForComponentPipelineToBeFinished (retriggered) failed for %s: %v", testPipelinerun.Name, err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					klog.Info("Konflux demo: retriggered build PipelineRun finished", "pipelineRun", testPipelinerun.Name)
				})
			})

			ginkgo.When("Integration Test PipelineRun is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should eventually complete successfully", func() {
					klog.Info("Konflux demo: waiting for Integration pipeline to finish", "scenario", integrationTestScenario.Name, "snapshot", snapshot.Name)
					err = fw.AsKubeAdmin.IntegrationController.WaitForIntegrationPipelineToBeFinished(integrationTestScenario, snapshot, userNamespace)
					if err != nil {
						klog.Errorf("Konflux demo: WaitForIntegrationPipelineToBeFinished failed for snapshot %s: %v", snapshot.GetName(), err)
						gomega.Expect(err).NotTo(gomega.HaveOccurred())
					}
					klog.Info("Konflux demo: Integration pipeline finished", "snapshot", snapshot.Name)
				})
			})

			ginkgo.When("Integration Test PipelineRun completes successfully", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should lead to Snapshot CR being marked as passed", func() {
					klog.Info("Konflux demo: waiting for Snapshot to be marked as passed", "pipelineRun", pipelineRun.Name)
					gomega.Eventually(func() bool {
						snapshot, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", pipelineRun.Name, "", userNamespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetSnapshot for pipelineRun %s failed: %v", pipelineRun.Name, err)
							gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
						}
						return fw.AsKubeAdmin.CommonController.HaveTestsSucceeded(snapshot)
					}, time.Minute*5, defaultPollingInterval).Should(gomega.BeTrue(), fmt.Sprintf("tests have not succeeded for snapshot %s/%s", snapshot.GetNamespace(), snapshot.GetName()))
					klog.Info("Konflux demo: Snapshot marked as passed", "snapshot", snapshot.Name)
				})

				ginkgo.It("should trigger creation of Release CR", func() {
					klog.Info("Konflux demo: waiting for Release CR", "snapshot", snapshot.Name, "namespace", userNamespace)
					gomega.Eventually(func() error {
						release, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshot.Name, userNamespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetRelease for snapshot %s failed: %v", snapshot.Name, err)
							return err
						}
						return nil
					}, releaseTimeout, releasePollingInterval).Should(gomega.Succeed(), fmt.Sprintf("timed out when trying to check if the release exists for snapshot %s/%s", userNamespace, snapshot.GetName()))
					klog.Info("Konflux demo: Release CR created", "release", release.Name)
				})
			})

			ginkgo.When("Release CR is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("triggers creation of Release PipelineRun", func() {
					klog.Info("Konflux demo: waiting for Release PipelineRun to start", "release", release.Name, "managedNamespace", managedNamespace)
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, release.Name, release.Namespace)
						if err != nil {
							ginkgo.GinkgoWriter.Printf("pipelineRun for component '%s' in namespace '%s' not created yet: %+v\n", component.GetName(), managedNamespace, err)
							klog.Errorf("Konflux demo: GetPipelineRunInNamespace for release %s failed: %v", release.Name, err)
							return err
						}
						if !pipelineRun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, pipelineRunStartedTimeout, defaultPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("failed to get pipelinerun named %q in namespace %q with label to release %q in namespace %q to start", pipelineRun.Name, managedNamespace, release.Name, release.Namespace))
					klog.Info("Konflux demo: Release PipelineRun started", "pipelineRun", pipelineRun.Name)
				})
			})

			ginkgo.When("Release PipelineRun is triggered", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should eventually succeed", func() {
					klog.Info("Konflux demo: waiting for Release PipelineRun to complete", "release", release.Name, "managedNamespace", managedNamespace)
					gomega.Eventually(func() error {
						pr, err := fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, release.Name, release.Namespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetPipelineRunInNamespace failed: %v", err)
							return err
						}
						if tekton.HasPipelineRunFailed(pr) {
							klog.Errorf("Konflux demo: Release PipelineRun %s/%s has failed", pr.GetNamespace(), pr.GetName())
							gomega.Expect(tekton.HasPipelineRunFailed(pr)).NotTo(gomega.BeTrue(), fmt.Sprintf("did not expect PipelineRun %s/%s to fail", pr.GetNamespace(), pr.GetName()))
						}
						if !pr.IsDone() {
							return fmt.Errorf("release pipelinerun %s/%s has not finished yet", pr.GetNamespace(), pr.GetName())
						}
						if !tekton.HasPipelineRunSucceeded(pr) {
							klog.Errorf("Konflux demo: Release PipelineRun %s/%s did not succeed", pr.GetNamespace(), pr.GetName())
							gomega.Expect(tekton.HasPipelineRunSucceeded(pr)).To(gomega.BeTrue(), fmt.Sprintf("PipelineRun %s/%s did not succeed", pr.GetNamespace(), pr.GetName()))
						}
						return nil
					}, releasePipelineTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("failed to see pipelinerun %q in namespace %q with a label pointing to release %q in namespace %q to complete successfully", pipelineRun.Name, managedNamespace, release.Name, release.Namespace))
					klog.Info("Konflux demo: Release PipelineRun succeeded", "release", release.Name)
				})
			})

			ginkgo.When("Release PipelineRun is completed", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should lead to Release CR being marked as succeeded", func() {
					klog.Info("Konflux demo: waiting for Release CR to be marked as released", "release", release.Name, "namespace", userNamespace)
					gomega.Eventually(func() error {
						release, err = fw.AsKubeAdmin.ReleaseController.GetRelease(release.Name, "", userNamespace)
						if err != nil {
							klog.Errorf("Konflux demo: GetRelease %s failed: %v", release.Name, err)
							return err
						}
						if !release.IsReleased() {
							return fmt.Errorf("release CR %s/%s is not marked as finished yet", release.GetNamespace(), release.GetName())
						}
						return nil
					}, customResourceUpdateTimeout, defaultPollingInterval).Should(gomega.Succeed(), fmt.Sprintf("failed to see release %q in namespace %q get marked as released", release.Name, userNamespace))
					klog.Info("Konflux demo: Release CR marked as succeeded", "release", release.Name)
				})
			})
		})
	}
})

func createReleaseConfig(kubeadminClient *framework.ControllerHub, managedNamespace, userNamespace, componentName, appName string, secretData []byte, ociStorage string) {
	var err error
	// Fallback: read env here in case the param was empty (e.g. different process context or env not inherited).
	if ociStorage == "" {
		ociStorage = os.Getenv("RELEASE_TA_OCI_STORAGE")
	}
	if ociStorage != "" {
		klog.Info("Konflux demo createReleaseConfig: RELEASE_TA_OCI_STORAGE set, will pass to RPA", "ociStorage", ociStorage)
		ginkgo.GinkgoWriter.Printf("RELEASE_TA_OCI_STORAGE=%q (release pipeline will push trusted artifacts here)\n", ociStorage)
	} else {
		klog.Info("Konflux demo createReleaseConfig: RELEASE_TA_OCI_STORAGE not set, release pipeline will use default ociStorage")
		ginkgo.GinkgoWriter.Printf("RELEASE_TA_OCI_STORAGE is not set; release pipeline will use default (quay.io/konflux-ci/...). Set it in e2e.env and export for local runs.\n")
	}

	klog.Info("Konflux demo createReleaseConfig: creating managed namespace", "managedNamespace", managedNamespace)
	_, err = kubeadminClient.CommonController.CreateTestNamespace(managedNamespace)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateTestNamespace %s failed: %v", managedNamespace, err)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "release-pull-secret", Namespace: managedNamespace},
		Data: map[string][]byte{".dockerconfigjson": secretData},
		Type: corev1.SecretTypeDockerConfigJson,
	}
	_, err = kubeadminClient.CommonController.CreateSecret(managedNamespace, secret)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateSecret release-pull-secret in %s failed: %v", managedNamespace, err)
		gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
	}

	klog.Info("Konflux demo createReleaseConfig: creating release-service-account", "managedNamespace", managedNamespace)
	managedServiceAccount, err := kubeadminClient.CommonController.CreateServiceAccount("release-service-account", managedNamespace, []corev1.ObjectReference{{Name: secret.Name}}, nil)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateServiceAccount failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(userNamespace, managedServiceAccount)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateReleasePipelineRoleBindingForServiceAccount in %s failed: %v", userNamespace, err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	_, err = kubeadminClient.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(managedNamespace, managedServiceAccount)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateReleasePipelineRoleBindingForServiceAccount in %s failed: %v", managedNamespace, err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	publicKey, err := kubeadminClient.TektonController.GetTektonChainsPublicKey()
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: GetTektonChainsPublicKey failed: %v", err)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	err = kubeadminClient.TektonController.CreateOrUpdateSigningSecret(publicKey, "cosign-public-key", managedNamespace)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateOrUpdateSigningSecret failed in %s: %v", managedNamespace, err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.ReleaseController.CreateReleasePlan("source-releaseplan", userNamespace, appName, managedNamespace, "", nil, nil, nil)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateReleasePlan failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	defaultEcPolicy, err := kubeadminClient.TektonController.GetEnterpriseContractPolicy("default", "enterprise-contract-service")
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: GetEnterpriseContractPolicy failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	ecPolicyName := componentName + "-policy"
	defaultEcPolicySpec := ecp.EnterpriseContractPolicySpec{
		Description: "Red Hat's enterprise requirements",
		PublicKey:   string(publicKey),
		Sources:     defaultEcPolicy.Spec.Sources,
		Configuration: &ecp.EnterpriseContractPolicyConfiguration{
			Collections: []string{"minimal"},
			Exclude:     []string{"cve"},
		},
	}
	_, err = kubeadminClient.TektonController.CreateEnterpriseContractPolicy(ecPolicyName, managedNamespace, defaultEcPolicySpec)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateEnterpriseContractPolicy %s failed: %v", ecPolicyName, err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.ReleaseController.CreateReleasePlanAdmission("demo", managedNamespace, "", userNamespace, ecPolicyName, "release-service-account", []string{appName}, false, &tektonutils.PipelineRef{
		Resolver: "git",
		Params: []tektonutils.Param{
			{Name: "url", Value: releasecommon.RelSvcCatalogURL},
			{Name: "revision", Value: releasecommon.RelSvcCatalogRevision},
			{Name: "pathInRepo", Value: "pipelines/managed/e2e/e2e.yaml"},
		},
		OciStorage: ociStorage,
	}, nil)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateReleasePlanAdmission failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.TektonController.CreatePVCInAccessMode("release-pvc", managedNamespace, corev1.ReadWriteOnce)
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreatePVCInAccessMode failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.CommonController.CreateRole("role-release-service-account", managedNamespace, map[string][]string{
		"apiGroupsList": {""},
		"roleResources": {"secrets"},
		"roleVerbs":     {"get", "list", "watch"},
	})
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateRole failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	_, err = kubeadminClient.CommonController.CreateRoleBinding("role-release-service-account-binding", managedNamespace, "ServiceAccount", "release-service-account", managedNamespace, "Role", "role-release-service-account", "rbac.authorization.k8s.io")
	if err != nil {
		klog.Errorf("Konflux demo createReleaseConfig: CreateRoleBinding failed: %v", err)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}
	klog.Info("Konflux demo createReleaseConfig: finished", "managedNamespace", managedNamespace, "userNamespace", userNamespace)
}

func CreateE2EQuaySecret(k *kubeapi.CustomClient) (*corev1.Secret, error) {
	var secret *corev1.Secret

	quayToken := os.Getenv("QUAY_TOKEN")
	if quayToken == "" {
		klog.Error("Konflux demo CreateE2EQuaySecret: QUAY_TOKEN env is not set")
		return nil, fmt.Errorf("failed to obtain quay token from 'QUAY_TOKEN' env; make sure the env exists")
	}

	decodedToken, err := base64.StdEncoding.DecodeString(quayToken)
	if err != nil {
		klog.Errorf("Konflux demo CreateE2EQuaySecret: failed to decode QUAY_TOKEN (base64): %v", err)
		return nil, fmt.Errorf("failed to decode quay token. Make sure that QUAY_TOKEN env contain a base64 token")
	}

	namespace := constants.QuayRepositorySecretNamespace
	klog.Info("Konflux demo CreateE2EQuaySecret: ensuring namespace exists", "namespace", namespace)
	_, err = k.KubeInterface().CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			_, err := k.KubeInterface().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				klog.Errorf("Konflux demo CreateE2EQuaySecret: failed to create namespace %s: %v", namespace, err)
				return nil, fmt.Errorf("error when creating namespace %s : %v", namespace, err)
			}
			klog.Info("Konflux demo CreateE2EQuaySecret: created namespace", "namespace", namespace)
		} else {
			klog.Errorf("Konflux demo CreateE2EQuaySecret: failed to get namespace %s: %v", namespace, err)
			return nil, fmt.Errorf("error when getting namespace %s : %v", namespace, err)
		}
	}

	secretName := constants.QuayRepositorySecretName
	secret, err = k.KubeInterface().CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})

	if err != nil {
		if k8sErrors.IsNotFound(err) {
			klog.Info("Konflux demo CreateE2EQuaySecret: creating secret", "secret", secretName, "namespace", namespace)
			secret, err = k.KubeInterface().CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
				Type: corev1.SecretTypeDockerConfigJson,
				Data: map[string][]byte{
					corev1.DockerConfigJsonKey: decodedToken,
				},
			}, metav1.CreateOptions{})

			if err != nil {
				klog.Errorf("Konflux demo CreateE2EQuaySecret: failed to create secret %s in %s: %v", secretName, namespace, err)
				return nil, fmt.Errorf("error when creating secret %s : %v", secretName, err)
			}
			klog.Info("Konflux demo CreateE2EQuaySecret: created secret", "secret", secretName)
		} else {
			secret.Data = map[string][]byte{
				corev1.DockerConfigJsonKey: decodedToken,
			}
			secret, err = k.KubeInterface().CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
			if err != nil {
				klog.Errorf("Konflux demo CreateE2EQuaySecret: failed to update secret %s: %v", secretName, err)
				return nil, fmt.Errorf("error when updating secret '%s' namespace: %v", secretName, err)
			}
			klog.Info("Konflux demo CreateE2EQuaySecret: updated secret", "secret", secretName)
		}
	}

	return secret, nil
}
