package has

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devfile/library/v2/pkg/util"
	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/tekton"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	imagecontroller "github.com/konflux-ci/image-controller/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	pointer "k8s.io/utils/ptr"
	"knative.dev/pkg/apis"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	RequiredLabelNotFound = "cannot retrigger PipelineRun - required label %q not found"
)

// GetComponent return a component object from kubernetes cluster
func (h *Controller) GetComponent(name string, namespace string) (*appservice.Component, error) {
	component := &appservice.Component{}
	if err := h.KubeRest().Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, component); err != nil {
		return nil, err
	}

	return component, nil
}

// GetComponentByApplicationName returns a component from kubernetes cluster given a application name.
func (h *Controller) GetComponentByApplicationName(applicationName string, namespace string) (*appservice.Component, error) {
	components := &appservice.ComponentList{}
	opts := []rclient.ListOption{
		rclient.InNamespace(namespace),
	}
	err := h.KubeRest().List(context.Background(), components, opts...)
	if err != nil {
		return nil, err
	}
	for _, component := range components.Items {
		if component.Spec.Application == applicationName {
			return &component, nil
		}
	}

	return &appservice.Component{}, fmt.Errorf("no component found %s", utils.GetAdditionalInfo(applicationName, namespace))
}

// GetComponentPipeline returns first pipeline run for a given component labels
func (h *Controller) GetComponentPipelineRun(componentName, applicationName, namespace, sha string) (*pipeline.PipelineRun, error) {
	return h.GetComponentPipelineRunWithType(componentName, applicationName, namespace, "", sha, "")
}

// GetComponentPipelineRunWithType returns first pipeline run for a given component labels with pipeline type within label "pipelines.appstudio.openshift.io/type" ("build", "test")
func (h *Controller) GetComponentPipelineRunWithType(componentName string, applicationName string, namespace, pipelineType string, sha string, eventType string) (*pipeline.PipelineRun, error) {
	prs, err := h.GetComponentPipelineRunsWithType(componentName, applicationName, namespace, pipelineType, sha, eventType)
	if err != nil {
		return nil, err
	} else {
		prsVal := *prs
		return &prsVal[0], nil
	}
}

// GetComponentPipelineRunsWithType returns all pipeline runs for a given component labels with pipeline type within label "pipelines.appstudio.openshift.io/type" ("build", "test")
func (h *Controller) GetComponentPipelineRunsWithType(componentName string, applicationName string, namespace, pipelineType string, sha string, eventType string) (*[]pipeline.PipelineRun, error) {
	pipelineRunLabels := map[string]string{"appstudio.openshift.io/component": componentName, "appstudio.openshift.io/application": applicationName}
	if pipelineType != "" {
		pipelineRunLabels["pipelines.appstudio.openshift.io/type"] = pipelineType
	}

	if sha != "" {
		pipelineRunLabels["pipelinesascode.tekton.dev/sha"] = sha
	}

	if eventType != "" {
		pipelineRunLabels["pipelinesascode.tekton.dev/event-type"] = eventType
	}

	list := &pipeline.PipelineRunList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(pipelineRunLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing pipelineruns in %s namespace: %v", namespace, err)
	}

	// If we hit any other error, while fetching pipelineRun list
	if err != nil {
		return nil, fmt.Errorf("error while trying to get pipelinerun list in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return &list.Items, nil
	}

	return nil, fmt.Errorf("no pipelinerun found for component %s", componentName)
}

// GetAllPipelineRunsForApplication returns the pipelineruns for a given application in the namespace
func (h *Controller) GetAllPipelineRunsForApplication(applicationName, namespace string) (*pipeline.PipelineRunList, error) {
	pipelineRunLabels := map[string]string{"appstudio.openshift.io/application": applicationName}

	list := &pipeline.PipelineRunList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(pipelineRunLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing pipelineruns in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return list, nil
	}

	return nil, fmt.Errorf("no pipelinerun found for application %s", applicationName)
}

// GetAllGroupSnapshotsForApplication returns the groupSnapshots for a given application in the namespace
func (h *Controller) GetAllGroupSnapshotsForApplication(applicationName, namespace string) (*appservice.SnapshotList, error) {
	snapshotLabels := map[string]string{"appstudio.openshift.io/application": applicationName, "test.appstudio.openshift.io/type": "group"}

	list := &appservice.SnapshotList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(snapshotLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing snapshots in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return list, nil
	}

	return nil, fmt.Errorf("no snapshot found for application %s", applicationName)
}

// GetAllComponentSnapshotsForApplication returns the gcomponentSnapshots for a given application in the namespace
func (h *Controller) GetAllComponentSnapshotsForApplication(applicationName, namespace string) (*appservice.SnapshotList, error) {
	snapshotLabels := map[string]string{"appstudio.openshift.io/application": applicationName, "test.appstudio.openshift.io/type": "component"}

	list := &appservice.SnapshotList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(snapshotLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing snapshots in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return list, nil
	}

	return nil, fmt.Errorf("no snapshot found for application %s", applicationName)
}

// GetAllComponentSnapshotsForApplicationAndComponent returns the component Snapshots for a given application and component in the namespace
func (h *Controller) GetAllComponentSnapshotsForApplicationAndComponent(applicationName, namespace, componentName string) (*[]appservice.Snapshot, error) {
	snapshotLabels := map[string]string{"appstudio.openshift.io/application": applicationName, "test.appstudio.openshift.io/type": "component", "appstudio.openshift.io/component": componentName}

	list := &appservice.SnapshotList{}
	err := h.KubeRest().List(context.Background(), list, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(snapshotLabels), Namespace: namespace})

	if err != nil && !k8sErrors.IsNotFound(err) {
		return nil, fmt.Errorf("error listing snapshots in %s namespace: %v", namespace, err)
	}

	if len(list.Items) > 0 {
		return &list.Items, nil
	}

	return nil, fmt.Errorf("no snapshot found for application %s and component %s", applicationName, componentName)
}

// Set of options to retrigger pipelineRuns in CI to fight against flakynes
type RetryOptions struct {
	// Indicate how many times a pipelineRun should be retriggered in case of flakines
	Retries int

	// If is set to true the PipelineRun will be retriggered always in case if pipelinerun fail for any reason. Time to time in RHTAP CI
	// we see that there are a lot of components which fail with QPS in build-container which cannot be controlled.
	// By default is false will retrigger a pipelineRun only when meet CouldntGetTask or TaskRunImagePullFailed conditions
	Always bool
}

// WaitForComponentPipelineToBeFinished waits for a given component PipelineRun to be finished
// In case of hitting issues like `TaskRunImagePullFailed` or `CouldntGetTask` it will re-trigger the PLR.
// Due to re-trigger mechanism this function can invalidate the related PLR object which might be used later in the test
// (by deleting the original PLR and creating a new one in case the PLR fails on one of the attempts).
// For that case this function gives an option to pass in a pointer to a related PLR object (`prToUpdate`) which will be updated (with a valid PLR object) before the end of this function
// and the PLR object can be then used for making assertions later in the test.
// If there's no intention for using the original PLR object later in the test, use `nil` instead of the pointer.
func (h *Controller) WaitForComponentPipelineToBeFinished(component *appservice.Component, pipelineType, sha, eventType string, t *tekton.Controller, r *RetryOptions, prToUpdate *pipeline.PipelineRun) error {
	attempts := 1
	app := component.Spec.Application
	pr := &pipeline.PipelineRun{}

	// Fail fast if the PipelineRun is never created.
	// Without this, we burn the full 30-minute completion timeout
	// just polling for a resource that may never appear.
	const pipelineRunCreationTimeout = 5 * time.Minute

	for {
		creationDeadline := time.Now().Add(pipelineRunCreationTimeout)
		prFound := false

		err := wait.PollUntilContextTimeout(context.Background(), constants.PipelineRunPollingInterval, 30*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			pr, err = h.GetComponentPipelineRunWithType(component.GetName(), app, component.GetNamespace(), pipelineType, sha, eventType)

			if err != nil {
				if !prFound && time.Now().After(creationDeadline) {
					return false, fmt.Errorf("PipelineRun was not created for Component %s/%s within %v",
						component.GetNamespace(), component.GetName(), pipelineRunCreationTimeout)
				}
				ginkgo.GinkgoWriter.Printf("PipelineRun has not been created yet for the Component %s/%s\n", component.GetNamespace(), component.GetName())
				return false, nil
			}

			if !prFound {
				prFound = true
				ginkgo.GinkgoWriter.Printf("PipelineRun %s found for Component %s/%s\n", pr.Name, component.GetNamespace(), component.GetName())
			}

			ginkgo.GinkgoWriter.Printf("PipelineRun %s reason: %s\n", pr.Name, pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason())

			if !pr.IsDone() {
				return false, nil
			}

			if pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue() {
				return true, nil
			}

			var prLogs string
			if err = t.StorePipelineRun(component.GetName(), pr); err != nil {
				ginkgo.GinkgoWriter.Printf("failed to store PipelineRun %s:%s: %s\n", pr.GetNamespace(), pr.GetName(), err.Error())
			}
			if prLogs, err = t.GetPipelineRunLogs(component.GetName(), pr.Name, pr.Namespace); err != nil {
				ginkgo.GinkgoWriter.Printf("failed to get logs for PipelineRun %s:%s: %s\n", pr.GetNamespace(), pr.GetName(), err.Error())
			}
			// Use condition reason and message when logs are empty (e.g. CouldntGetTask leaves no TaskRuns)
			if strings.TrimSpace(prLogs) == "" {
				cond := pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded)
				prLogs = cond.GetReason()
				if msg := cond.GetMessage(); msg != "" {
					prLogs = prLogs + ": " + msg
				}
			}
			return false, fmt.Errorf("%s", prLogs)
		})

		if err != nil {
			if !prFound {
				return fmt.Errorf("PipelineRun cannot be created for the Component %s/%s", component.GetNamespace(), component.GetName())
			}
			ginkgo.GinkgoWriter.Printf("attempt %d/%d: PipelineRun %q failed: %+v", attempts, r.Retries+1, pr.GetName(), err)
			// CouldntGetTask: Retry the PipelineRun only in case we hit the known issue https://issues.redhat.com/browse/SRVKP-2749
			// TaskRunImagePullFailed: Retry in case of https://issues.redhat.com/browse/RHTAPBUGS-985 and https://github.com/tektoncd/pipeline/issues/7184
			if attempts == r.Retries+1 || (!r.Always && pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason() != "CouldntGetTask" && pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).GetReason() != "TaskRunImagePullFailed") {
				return err
			}
			if err = t.RemoveFinalizerFromPipelineRun(pr, constants.E2ETestFinalizerName); err != nil {
				return fmt.Errorf("failed to remove the finalizer from pipelinerun %s:%s in order to retrigger it: %+v", pr.GetNamespace(), pr.GetName(), err)
			}
			if err = h.PipelineClient().TektonV1().PipelineRuns(pr.GetNamespace()).Delete(context.Background(), pr.GetName(), metav1.DeleteOptions{}); err != nil {
				return fmt.Errorf("failed to delete PipelineRun %q from %q namespace with error: %v", pr.GetName(), pr.GetNamespace(), err)
			}
			if sha, err = h.RetriggerComponentPipelineRun(component, pr); err != nil {
				return fmt.Errorf("unable to retrigger pipelinerun for component %s:%s: %+v", component.GetNamespace(), component.GetName(), err)
			}
			// Clear event-type filter after retrigger: the retrigger mechanism (e.g. git push)
			// may produce a PipelineRun with a different event-type than the original (e.g.
			// "push" instead of "incoming"). The new sha is sufficient to identify the right PLR.
			eventType = ""
			attempts++
		} else {
			break
		}
	}

	// If prToUpdate variable was passed to this function, update it with the latest version of the PipelineRun object
	if prToUpdate != nil {
		pr.DeepCopyInto(prToUpdate)
	}

	return nil
}

// Universal method to create a component in the kubernetes clusters.
func (h *Controller) CreateComponent(componentSpec appservice.ComponentSpec, namespace string, outputContainerImage string, secret string, applicationName string, skipInitialChecks bool, annotations map[string]string) (*appservice.Component, error) {
	componentObject := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentSpec.ComponentName,
			Namespace: namespace,
			Annotations: map[string]string{
				"skip-initial-checks": strconv.FormatBool(skipInitialChecks),
			},
		},
		Spec: componentSpec,
	}
	componentObject.Spec.Secret = secret
	componentObject.Spec.Application = applicationName

	if len(annotations) > 0 {
		componentObject.Annotations = utils.MergeMaps(componentObject.Annotations, annotations)

	}

	if componentObject.Spec.TargetPort == 0 {
		componentObject.Spec.TargetPort = 8081
	}
	if outputContainerImage != "" {
		componentObject.Spec.ContainerImage = outputContainerImage
	} else if componentObject.Annotations["image.redhat.com/generate"] == "" {
		// Generate default public image repo since nothing is mentioned specifically
		componentObject.Annotations = utils.MergeMaps(componentObject.Annotations, constants.ImageControllerAnnotationRequestPublicRepo)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*1)
	defer cancel()
	if err := h.KubeRest().Create(ctx, componentObject); err != nil {
		return nil, err
	}

	return componentObject, nil
}

// Create a component and check image repository gets created.
func (h *Controller) CreateComponentCheckImageRepository(componentSpec appservice.ComponentSpec, namespace string, outputContainerImage string, secret string, applicationName string, skipInitialChecks bool, annotations map[string]string) (*appservice.Component, error) {
	componentObject, err := h.CreateComponent(componentSpec, namespace, outputContainerImage, secret, applicationName, skipInitialChecks, annotations)
	if err != nil {
		return nil, err
	}

	// Decrease the timeout to 5 mins, when the issue https://issues.redhat.com/browse/STONEBLD-3552 is fixed
	if err := utils.WaitUntilWithInterval(h.CheckImageRepositoryExists(namespace, componentSpec.ComponentName), time.Second*10, time.Minute*15); err != nil {
		return nil, fmt.Errorf("timed out waiting for image repository to be ready for component %s in namespace %s: %+v", componentSpec.ComponentName, namespace, err)
	}

	return componentObject, nil
}

// CreateComponentWithDockerSource creates a component based on container image source.
func (h *Controller) CreateComponentWithDockerSource(applicationName, componentName, namespace, gitSourceURL, containerImageSource, outputContainerImage, secret string) (*appservice.Component, error) {
	component := &appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentName,
			Namespace: namespace,
		},
		Spec: appservice.ComponentSpec{
			ComponentName: componentName,
			Application:   applicationName,
			Source: appservice.ComponentSource{
				ComponentSourceUnion: appservice.ComponentSourceUnion{
					GitSource: &appservice.GitSource{
						URL:           gitSourceURL,
						DockerfileURL: containerImageSource,
					},
				},
			},
			Secret:         secret,
			ContainerImage: outputContainerImage,
			Replicas:       pointer.To[int](1),
			TargetPort:     8081,
			Route:          "",
		},
	}
	err := h.KubeRest().Create(context.Background(), component)
	if err != nil {
		return nil, err
	}
	return component, nil
}

// ScaleDeploymentReplicas scales the replicas of a given deployment
func (h *Controller) ScaleComponentReplicas(component *appservice.Component, replicas *int) (*appservice.Component, error) {
	component.Spec.Replicas = replicas

	err := h.KubeRest().Update(context.Background(), component, &rclient.UpdateOptions{})
	if err != nil {
		return &appservice.Component{}, err
	}
	return component, nil
}

// DeleteComponent delete an has component from a given name and namespace
func (h *Controller) DeleteComponent(name string, namespace string, reportErrorOnNotFound bool) error {
	component := appservice.Component{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := h.KubeRest().Delete(context.Background(), &component); err != nil {
		if !k8sErrors.IsNotFound(err) || (k8sErrors.IsNotFound(err) && reportErrorOnNotFound) {
			return fmt.Errorf("error deleting a component: %+v", err)
		}
	}

	// RHTAPBUGS-978: temporary timeout to 15min
	err := utils.WaitUntil(h.ComponentDeleted(&component), 15*time.Minute)

	return err
}

// DeleteAllComponentsInASpecificNamespace removes all component CRs from a specific namespace. Useful when creating a lot of resources and want to remove all of them
func (h *Controller) DeleteAllComponentsInASpecificNamespace(namespace string, timeout time.Duration) error {
	if err := h.KubeRest().DeleteAllOf(context.Background(), &appservice.Component{}, rclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting components from the namespace %s: %+v", namespace, err)
	}

	componentList := &appservice.ComponentList{}

	err := utils.WaitUntil(func() (done bool, err error) {
		if err := h.KubeRest().List(context.Background(), componentList, &rclient.ListOptions{Namespace: namespace}); err != nil {
			return false, nil
		}
		return len(componentList.Items) == 0, nil
	}, timeout)

	return err
}

// Waits for a component until is deleted and if not will return an error
func (h *Controller) ComponentDeleted(component *appservice.Component) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := h.GetComponent(component.Name, component.Namespace)
		return err != nil && k8sErrors.IsNotFound(err), nil
	}
}

// Get the message from the status of a component. Usefull for debugging purposes.
func (h *Controller) GetComponentConditionStatusMessages(name, namespace string) (messages []string, err error) {
	c, err := h.GetComponent(name, namespace)
	if err != nil {
		return messages, fmt.Errorf("error getting HAS component: %v", err)
	}
	for _, condition := range c.Status.Conditions {
		messages = append(messages, condition.Message)
	}
	return
}

// Universal method to retrigger pipelineruns in kubernetes cluster
func (h *Controller) RetriggerComponentPipelineRun(component *appservice.Component, pr *pipeline.PipelineRun) (sha string, err error) {
	prLabels := pr.GetLabels()
	prAnnotations := pr.GetAnnotations()
	// In case of PipelineRun managed by PaC we are able to retrigger the pipeline only
	// by updating the related branch
	if prLabels["app.kubernetes.io/managed-by"] == "pipelinesascode.tekton.dev" {
		var ok bool
		var repoName, eventType, branchName, gitProvider string
		pacRepoNameLabelName := "pipelinesascode.tekton.dev/url-repository"
		gitProviderLabelName := "pipelinesascode.tekton.dev/git-provider"
		pacEventTypeLabelName := "pipelinesascode.tekton.dev/event-type"
		componentLabelName := "appstudio.openshift.io/component"
		targetBranchAnnotationName := "build.appstudio.redhat.com/target_branch"

		if repoName, ok = prLabels[pacRepoNameLabelName]; !ok {
			return "", fmt.Errorf(RequiredLabelNotFound, pacRepoNameLabelName)
		}
		if eventType, ok = prLabels[pacEventTypeLabelName]; !ok {
			return "", fmt.Errorf(RequiredLabelNotFound, pacEventTypeLabelName)
		}
		// since not all build PipelineRuns contains this annotation
		gitProvider = prAnnotations[gitProviderLabelName]

		// PipelineRun is triggered from a pull request, need to update the PaC PR source branch
		if eventType == "pull_request" || eventType == "Merge_Request" {
			if len(prLabels[componentLabelName]) < 1 {
				return "", fmt.Errorf(RequiredLabelNotFound, componentLabelName)
			}
			branchName = constants.PaCPullRequestBranchPrefix + prLabels[componentLabelName]
		} else {
			// No straightforward way to get a target branch from PR labels -> using annotation
			if branchName, ok = pr.GetAnnotations()[targetBranchAnnotationName]; !ok {
				return "", fmt.Errorf("cannot retrigger PipelineRun - required annotation %q not found", targetBranchAnnotationName)
			}
		}

		if gitProvider == "gitlab" {
			gitlabOrg := utils.GetEnv(constants.GITLAB_QE_ORG_ENV, constants.DefaultGitLabQEOrg)
			projectID, ok := prLabels["pipelinesascode.tekton.dev/source-project-id"]
			if !ok {
				projectID = fmt.Sprintf("%s/%s", gitlabOrg, repoName)
			}
			fileInfo, err := h.GitLab.CreateFile(projectID, util.GenerateRandomString(5), "test", branchName)
			if err != nil {
				return "", fmt.Errorf("failed to retrigger PipelineRun %s in %s namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
			}
			file, err := h.GitLab.GetFileMetaData(projectID, fileInfo.FilePath, fileInfo.Branch)
			if err != nil {
				return "", fmt.Errorf("failed to retrigger PipelineRun %s in %s namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
			}
			sha = file.CommitID
		} else {
			file, err := h.GitHub.CreateFile(repoName, util.GenerateRandomString(5), "test", branchName)
			if err != nil {
				return "", fmt.Errorf("failed to retrigger PipelineRun %s in %s namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
			}
			sha = file.GetSHA()
		}

		// To retrigger simple build PipelineRun we just need to update the initial build annotation
		// in Component CR
	} else {
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			component, err := h.GetComponent(component.GetName(), component.GetNamespace())
			if err != nil {
				return fmt.Errorf("failed to get component for PipelineRun %q in %q namespace: %+v", pr.GetName(), pr.GetNamespace(), err)
			}
			component.Annotations = utils.MergeMaps(component.Annotations, constants.ComponentTriggerSimpleBuildAnnotation)
			if err = h.KubeRest().Update(context.Background(), component); err != nil {
				return fmt.Errorf("failed to update Component %q in %q namespace", component.GetName(), component.GetNamespace())
			}
			return err
		})

		if err != nil {
			return "", err
		}
	}
	// Poll for the new PipelineRun instead of using a watch to avoid a race
	// condition where the PipelineRun is created between the retrigger action
	// and the watch setup (which would cause the watch to miss the event).
	deadline := time.After(5 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return "", fmt.Errorf("timed out waiting for new PipelineRun to appear after retriggering it for component %s:%s", component.GetNamespace(), component.GetName())
		case <-ticker.C:
			pipelineRuns, listErr := h.PipelineClient().TektonV1().PipelineRuns(component.GetNamespace()).List(context.Background(), metav1.ListOptions{})
			if listErr != nil {
				ginkgo.GinkgoWriter.Printf("failed to list PipelineRuns while waiting for retrigger: %v\n", listErr)
				continue
			}
			for i := range pipelineRuns.Items {
				newPR := &pipelineRuns.Items[i]
				if pr.GetGenerateName() == newPR.GetGenerateName() && pr.GetName() != newPR.GetName() {
					ginkgo.GinkgoWriter.Printf("New PipelineRun %s found after retrigger for component %s/%s\n", newPR.GetName(), component.GetNamespace(), component.GetName())
					return sha, nil
				}
			}
		}
	}
}

func (h *Controller) CheckImageRepositoryExists(namespace, componentName string) wait.ConditionFunc {
	return func() (bool, error) {
		imageRepositoryList := &imagecontroller.ImageRepositoryList{}
		imageRepoLabels := map[string]string{"appstudio.redhat.com/component": componentName}
		err := h.KubeRest().List(context.Background(), imageRepositoryList, &rclient.ListOptions{LabelSelector: labels.SelectorFromSet(imageRepoLabels), Namespace: namespace})
		if err != nil {
			return false, err
		}
		if len(imageRepositoryList.Items) == 0 {
			return false, nil
		}
		if len(imageRepositoryList.Items) > 1 {
			return false, fmt.Errorf("more than one image repositories found for component %s", componentName)
		}
		if imageRepositoryList.Items[0].Status.State != "ready" {
			ginkgo.GinkgoWriter.Printf("Image repository for component %s in namespace %s do not have right state ('%s' != 'ready') yet but it has status %v.\n", componentName, namespace, imageRepositoryList.Items[0].Status.State, imageRepositoryList.Items[0].Status)
			return false, nil
		}
		return true, nil
	}
}

// DeleteAllImageRepositoriesInASpecificNamespace removes all image repository CRs from a specific namespace. Useful when cleaning up a namespace and component cleanup did not cleaned it's image repository
func (h *Controller) DeleteAllImageRepositoriesInASpecificNamespace(namespace string, timeout time.Duration) error {
	if err := h.KubeRest().DeleteAllOf(context.Background(), &imagecontroller.ImageRepository{}, rclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting image repositories from the namespace %s: %+v", namespace, err)
	}

	imageRepositoryList := &imagecontroller.ImageRepositoryList{}

	err := utils.WaitUntil(func() (done bool, err error) {
		if err := h.KubeRest().List(context.Background(), imageRepositoryList, &rclient.ListOptions{Namespace: namespace}); err != nil {
			return false, nil
		}
		return len(imageRepositoryList.Items) == 0, nil
	}, timeout)

	return err
}

// Gets value of a specified annotation in a component
func (h *Controller) GetComponentAnnotation(componentName, annotationKey, namespace string) (string, error) {
	component, err := h.GetComponent(componentName, namespace)
	if err != nil {
		return "", fmt.Errorf("error when getting component: %+v", err)
	}
	return component.Annotations[annotationKey], nil
}

// Sets annotation in a component
func (h *Controller) SetComponentAnnotation(componentName, annotationKey, annotationValue, namespace string) error {
	component, err := h.GetComponent(componentName, namespace)
	if err != nil {
		return fmt.Errorf("error when getting component: %+v", err)
	}
	newAnnotations := component.GetAnnotations()
	newAnnotations[annotationKey] = annotationValue
	component.SetAnnotations(newAnnotations)
	err = h.KubeRest().Update(context.Background(), component)
	if err != nil {
		return fmt.Errorf("error when updating component: %+v", err)
	}
	return nil
}

// StoreComponent stores a given Component as an artifact.
func (h *Controller) StoreComponent(component *appservice.Component) error {
	artifacts := make(map[string][]byte)

	componentConditionStatus, err := h.GetComponentConditionStatusMessages(component.Name, component.Namespace)
	if err != nil {
		return err
	}
	artifacts["component-condition-status-"+component.Name+".log"] = []byte(strings.Join(componentConditionStatus, "\n"))

	componentYaml, err := yaml.Marshal(component)
	if err != nil {
		return err
	}
	artifacts["component-"+component.Name+".yaml"] = componentYaml

	if err := logs.StoreArtifacts(artifacts); err != nil {
		return err
	}

	return nil
}

// StoreAllComponents stores all Components in a given namespace.
func (h *Controller) StoreAllComponents(namespace string) error {
	componentList := &appservice.ComponentList{}
	if err := h.KubeRest().List(context.Background(), componentList, &rclient.ListOptions{Namespace: namespace}); err != nil {
		return err
	}

	for _, component := range componentList.Items {
		if err := h.StoreComponent(&component); err != nil {
			return err
		}
	}
	return nil
}

// UpdateComponent updates a component
func (h *Controller) UpdateComponent(component *appservice.Component) error {
	err := h.KubeRest().Update(context.Background(), component, &rclient.UpdateOptions{})

	if err != nil {
		return err
	}
	return nil
}
