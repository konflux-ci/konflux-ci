package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	buildcontrollers "github.com/konflux-ci/build-service/controllers"

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

	integrationv1beta2 "github.com/konflux-ci/integration-service/api/v1beta2"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonapi "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

	e2eConfig "github.com/konflux-ci/konflux-ci/test/go-tests/tests/conformance/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var _ = ginkgo.Describe("[conformance]", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
	defer ginkgo.GinkgoRecover()

	var timeout, interval time.Duration
	var userNamespace, managedNamespace string
	var err error

	var component *appservice.Component
	var release *releaseApi.Release
	var snapshot *appservice.Snapshot
	var pipelineRun, testPipelinerun *tektonapi.PipelineRun
	var integrationTestScenario *integrationv1beta2.IntegrationTestScenario

	var prNumber int
	var headSHA, pacBranchName string
	var mergeResult *github.PullRequestMergeResult

	fw := &framework.Framework{}
	var buildPipelineAnnotation map[string]string
	var componentNewBaseBranch, gitRevision, componentRepositoryName, componentName string

	for _, appSpec := range e2eConfig.UpstreamAppSpecs {
		appSpec := appSpec
		if appSpec.Skip {
			continue
		}

		ginkgo.Describe(appSpec.Name, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func() {
				if os.Getenv(constants.SKIP_PAC_TESTS_ENV) == "true" {
					ginkgo.Skip("Skipping: SKIP_PAC_TESTS is set")
				}

				fw, err = framework.NewFramework(utils.GetGeneratedNamespace(devEnvTestLabel))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())

				userNamespace = fw.UserNamespace
				managedNamespace = "default-managed-tenant"
				klog.Info("conformance: namespaces ready", "user", userNamespace, "managed", managedNamespace)

				componentName = fmt.Sprintf("%s-%s", appSpec.ComponentSpec.Name, util.GenerateRandomString(4))
				pacBranchName = constants.PaCPullRequestBranchPrefix + componentName
				componentRepositoryName = utils.ExtractGitRepositoryNameFromURL(appSpec.ComponentSpec.GitSourceUrl)

				gomega.Expect(runSetupRelease(appSpec.ApplicationName, componentName, userNamespace, managedNamespace)).To(gomega.Succeed())
				gomega.Expect(grantIntegrationRunnerJobRBAC(userNamespace)).To(gomega.Succeed())

				buildPipelineAnnotation = build.GetBuildPipelineBundleAnnotation(appSpec.ComponentSpec.BuildPipelineType)
			})

			ginkgo.AfterAll(func() {
				if strings.EqualFold(os.Getenv("E2E_SKIP_CLEANUP"), "true") || ginkgo.CurrentSpecReport().Failed() || strings.Contains(ginkgo.GinkgoLabelFilter(), upstreamKonfluxTestLabel) {
					return
				}
				klog.Info("conformance: cleaning up")
				_ = fw.AsKubeAdmin.CommonController.KubeInterface().CoreV1().Namespaces().Delete(context.Background(), managedNamespace, metav1.DeleteOptions{})
				cleanupWithRetry("delete PaC branch", func() error {
					return fw.AsKubeAdmin.CommonController.GitHub.DeleteRef(componentRepositoryName, pacBranchName)
				})
				cleanupWithRetry("delete base branch", func() error {
					return fw.AsKubeAdmin.CommonController.GitHub.DeleteRef(componentRepositoryName, componentNewBaseBranch)
				})
				cleanupWithRetry("cleanup webhooks", func() error {
					return build.CleanupWebhooks(fw, componentRepositoryName)
				})
			})

			// --- Application & Component Setup ---

			ginkgo.It("creates an application", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				createdApplication, createErr := fw.AsKubeAdmin.HasController.CreateApplication(appSpec.ApplicationName, userNamespace)
				gomega.Expect(createErr).NotTo(gomega.HaveOccurred())
				gomega.Expect(createdApplication.Spec.DisplayName).To(gomega.Equal(appSpec.ApplicationName))
				gomega.Expect(createdApplication.Namespace).To(gomega.Equal(userNamespace))
			})

			ginkgo.It("creates an IntegrationTestScenario", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				its := appSpec.ComponentSpec.IntegrationTestScenario
				gomega.Eventually(func() error {
					var createErr error
					integrationTestScenario, createErr = fw.AsKubeAdmin.IntegrationController.CreateIntegrationTestScenario("", appSpec.ApplicationName, userNamespace, its.GitURL, its.GitRevision, its.TestPath, "", []string{})
					return createErr
				}, time.Minute*2, time.Second*5).Should(gomega.Succeed(), "timed out creating IntegrationTestScenario")
			})

			ginkgo.It("creates new branch for the build", ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
				componentNewBaseBranch = fmt.Sprintf("base-%s", util.GenerateRandomString(6))
				gitRevision = componentNewBaseBranch
				err = fw.AsKubeAdmin.CommonController.GitHub.CreateRef(componentRepositoryName, appSpec.ComponentSpec.GitSourceDefaultBranchName, appSpec.ComponentSpec.GitSourceRevision, componentNewBaseBranch)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			})

			ginkgo.It(fmt.Sprintf("creates component %s (private: %t) from git source %s", appSpec.ComponentSpec.Name, appSpec.ComponentSpec.Private, appSpec.ComponentSpec.GitSourceUrl), ginkgo.Label(devEnvTestLabel, upstreamKonfluxTestLabel), func() {
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
				gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
			})

			// --- PaC Pull Request & Build ---

			ginkgo.When("Component is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("triggers creation of a PR in the sample repo", func() {
					var prSHA string
					gomega.Eventually(func() error {
						prs, listErr := fw.AsKubeAdmin.CommonController.GitHub.ListPullRequests(componentRepositoryName)
						if listErr != nil {
							return listErr
						}
						for _, pr := range prs {
							if pr.Head.GetRef() == pacBranchName {
								prNumber = pr.GetNumber()
								prSHA = pr.GetHead().GetSHA()
								return nil
							}
						}
						dumpDiagnostics(fw.AsKubeAdmin, component.GetName(), appSpec.ApplicationName, userNamespace)
						return fmt.Errorf("PaC branch %s not found among %d PRs", pacBranchName, len(prs))
					}, pullRequestCreationTimeout, defaultPollingInterval).Should(gomega.Succeed(), "timed out waiting for PaC PR")

					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, prSHA)
						if err != nil {
							dumpDiagnostics(fw.AsKubeAdmin, component.GetName(), appSpec.ApplicationName, userNamespace)
							return err
						}
						return fw.AsKubeAdmin.TektonController.DeletePipelineRun(pipelineRun.Name, pipelineRun.Namespace)
					}, pipelineRunStartedTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), "timed out waiting for pull-request PipelineRun")
				})

				ginkgo.It("verifies component build status", func() {
					var buildStatus *buildcontrollers.BuildStatus
					timeout = time.Minute * 5
					interval = defaultPollingInterval
					gomega.Eventually(func() (bool, error) {
						comp, getErr := fw.AsKubeAdmin.HasController.GetComponent(component.GetName(), userNamespace)
						if getErr != nil {
							return false, getErr
						}
						if err := json.Unmarshal([]byte(comp.Annotations[buildcontrollers.BuildStatusAnnotationName]), &buildStatus); err != nil {
							return false, err
						}
						if buildStatus.PaC == nil {
							return false, nil
						}
						return buildStatus.PaC.State == "enabled" && buildStatus.PaC.MergeUrl != "" && buildStatus.PaC.ErrId == 0 && buildStatus.PaC.ConfigurationTime != "", nil
					}, timeout, interval).Should(gomega.BeTrue(), "component build status has unexpected PaC state")
				})

				ginkgo.It("triggers a push PipelineRun after merging the PaC init branch", func() {
					gomega.Eventually(func() error {
						mergeResult, err = fw.AsKubeAdmin.CommonController.GitHub.MergePullRequest(componentRepositoryName, prNumber)
						return err
					}, mergePRTimeout).ShouldNot(gomega.HaveOccurred(), "failed to merge PaC PR")

					headSHA = mergeResult.GetSHA()

					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
						if err != nil {
							dumpDiagnostics(fw.AsKubeAdmin, component.GetName(), appSpec.ApplicationName, userNamespace)
							return err
						}
						if !pipelineRun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, pipelineRunStartedTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), "timed out waiting for push PipelineRun to start")
				})
			})

			// --- Build Validation ---

			ginkgo.When("Build PipelineRun is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("does not contain an annotation with a Snapshot Name", func() {
					gomega.Expect(pipelineRun.Annotations["appstudio.openshift.io/snapshot"]).To(gomega.Equal(""))
				})

				ginkgo.It("should eventually complete successfully", func() {
					err = fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "build", headSHA, "",
						fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 5, Always: true}, pipelineRun)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					headSHA = pipelineRun.Labels["pipelinesascode.tekton.dev/sha"]
				})
			})

			ginkgo.When("Build PipelineRun completes successfully", func() {
				ginkgo.It("should validate Tekton TaskRun test results successfully", func() {
					pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					err = build.ValidateBuildPipelineTestResults(pipelineRun, fw.AsKubeAdmin.CommonController.KubeRest(), false)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				})

				ginkgo.It("should validate that the build pipelineRun is signed", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
						if err != nil {
							return err
						}
						if pipelineRun.Annotations["chains.tekton.dev/signed"] != "true" {
							return fmt.Errorf("pipelinerun %s/%s is not signed", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, time.Minute*5, time.Second*5).Should(gomega.Succeed(), "build pipelineRun is not signed")
				})

				ginkgo.It("should find the related Snapshot CR", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					gomega.Eventually(func() error {
						snapshot, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", pipelineRun.Name, "", userNamespace)
						return err
					}, snapshotTimeout, snapshotPollingInterval).Should(gomega.Succeed(), "timed out waiting for Snapshot")
				})

				ginkgo.It("should validate that the build pipelineRun is annotated with the name of the Snapshot", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					pipelineRun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRun(component.GetName(), appSpec.ApplicationName, userNamespace, headSHA)
					gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
					gomega.Expect(pipelineRun.Annotations["appstudio.openshift.io/snapshot"]).To(gomega.Equal(snapshot.GetName()))
				})

				ginkgo.It("should find the related Integration Test PipelineRun", ginkgo.Label(upstreamKonfluxTestLabel), func() {
					gomega.Eventually(func() error {
						testPipelinerun, err = fw.AsKubeAdmin.IntegrationController.GetIntegrationPipelineRun(integrationTestScenario.Name, snapshot.Name, userNamespace)
						if err != nil {
							dumpDiagnostics(fw.AsKubeAdmin, component.GetName(), appSpec.ApplicationName, userNamespace)
							return err
						}
						if !testPipelinerun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", testPipelinerun.GetNamespace(), testPipelinerun.GetName())
						}
						return nil
					}, pipelineRunStartedTimeout, defaultPollingInterval).Should(gomega.Succeed())
					gomega.Expect(testPipelinerun.Labels["appstudio.openshift.io/snapshot"]).To(gomega.ContainSubstring(snapshot.Name))
					gomega.Expect(testPipelinerun.Labels["test.appstudio.openshift.io/scenario"]).To(gomega.ContainSubstring(integrationTestScenario.Name))
				})
			})

			// --- Build Retrigger ---

			ginkgo.When("push pipelinerun is retriggered", func() {
				ginkgo.It("should eventually succeed", func() {
					err = fw.AsKubeAdmin.HasController.SetComponentAnnotation(component.GetName(), buildcontrollers.BuildRequestAnnotationName, buildcontrollers.BuildRequestTriggerPaCBuildAnnotationValue, userNamespace)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())

					gomega.Eventually(func() error {
						testPipelinerun, err = fw.AsKubeAdmin.HasController.GetComponentPipelineRunWithType(component.GetName(), appSpec.ApplicationName, userNamespace, "build", "", "incoming")
						if err != nil {
							dumpDiagnostics(fw.AsKubeAdmin, component.GetName(), appSpec.ApplicationName, userNamespace)
							return err
						}
						if !testPipelinerun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", testPipelinerun.GetNamespace(), testPipelinerun.GetName())
						}
						return nil
					}, 10*time.Minute, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), "timed out waiting for retriggered PipelineRun")

					err = fw.AsKubeAdmin.HasController.WaitForComponentPipelineToBeFinished(component, "build", "", "incoming", fw.AsKubeAdmin.TektonController, &has.RetryOptions{Retries: 2, Always: true}, testPipelinerun)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				})
			})

			// --- Integration Testing ---

			ginkgo.When("Integration Test PipelineRun is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should eventually complete successfully", func() {
					err = fw.AsKubeAdmin.IntegrationController.WaitForIntegrationPipelineToBeFinished(integrationTestScenario, snapshot, userNamespace)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
				})
			})

			ginkgo.When("Integration Test PipelineRun completes successfully", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should lead to Snapshot CR being marked as passed", func() {
					gomega.Eventually(func() bool {
						snapshot, err = fw.AsKubeAdmin.IntegrationController.GetSnapshot("", pipelineRun.Name, "", userNamespace)
						gomega.Expect(err).ShouldNot(gomega.HaveOccurred())
						return fw.AsKubeAdmin.CommonController.HaveTestsSucceeded(snapshot)
					}, time.Minute*5, defaultPollingInterval).Should(gomega.BeTrue(), "tests have not succeeded for snapshot %s/%s", snapshot.GetNamespace(), snapshot.GetName())
				})

				ginkgo.It("should trigger creation of Release CR", func() {
					gomega.Eventually(func() error {
						release, err = fw.AsKubeAdmin.ReleaseController.GetRelease("", snapshot.Name, userNamespace)
						return err
					}, releaseTimeout, releasePollingInterval).Should(gomega.Succeed(), "timed out waiting for Release")
				})
			})

			// --- Release ---

			ginkgo.When("Release CR is created", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("triggers creation of Release PipelineRun", func() {
					gomega.Eventually(func() error {
						pipelineRun, err = fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, release.Name, release.Namespace)
						if err != nil {
							return err
						}
						if !pipelineRun.HasStarted() {
							return fmt.Errorf("pipelinerun %s/%s hasn't started yet", pipelineRun.GetNamespace(), pipelineRun.GetName())
						}
						return nil
					}, pipelineRunStartedTimeout, defaultPollingInterval).Should(gomega.Succeed(), "timed out waiting for Release PipelineRun")
				})
			})

			ginkgo.When("Release PipelineRun is triggered", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should eventually succeed", func() {
					gomega.Eventually(func() error {
						pr, getErr := fw.AsKubeAdmin.ReleaseController.GetPipelineRunInNamespace(managedNamespace, release.Name, release.Namespace)
						if getErr != nil {
							return getErr
						}
						if tekton.HasPipelineRunFailed(pr) {
							gomega.Expect(tekton.HasPipelineRunFailed(pr)).NotTo(gomega.BeTrue(), "PipelineRun %s/%s failed", pr.GetNamespace(), pr.GetName())
						}
						if !pr.IsDone() {
							return fmt.Errorf("release pipelinerun %s/%s has not finished yet", pr.GetNamespace(), pr.GetName())
						}
						gomega.Expect(tekton.HasPipelineRunSucceeded(pr)).To(gomega.BeTrue(), "PipelineRun %s/%s did not succeed", pr.GetNamespace(), pr.GetName())
						return nil
					}, releasePipelineTimeout, constants.PipelineRunPollingInterval).Should(gomega.Succeed(), "release PipelineRun did not complete successfully")
				})
			})

			ginkgo.When("Release PipelineRun is completed", ginkgo.Label(upstreamKonfluxTestLabel), func() {
				ginkgo.It("should lead to Release CR being marked as succeeded", func() {
					gomega.Eventually(func() error {
						release, err = fw.AsKubeAdmin.ReleaseController.GetRelease(release.Name, "", userNamespace)
						if err != nil {
							return err
						}
						if !release.IsReleased() {
							return fmt.Errorf("release %s/%s is not marked as released yet", release.GetNamespace(), release.GetName())
						}
						return nil
					}, customResourceUpdateTimeout, defaultPollingInterval).Should(gomega.Succeed(), "release %q not marked as released", release.Name)
				})
			})
		})
	}
})
