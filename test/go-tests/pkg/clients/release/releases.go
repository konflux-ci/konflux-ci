package release

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/has"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/tekton"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	ginkgo "github.com/onsi/ginkgo/v2"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"knative.dev/pkg/apis"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// CreateRelease creates a new Release using the given parameters.
func (r *Controller) CreateRelease(name, namespace, snapshot, releasePlan string) (*releaseApi.Release, error) {
	release := &releaseApi.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: releaseApi.ReleaseSpec{
			Snapshot:    snapshot,
			ReleasePlan: releasePlan,
		},
	}

	return release, r.KubeRest().Create(context.Background(), release)
}

// CreateReleasePipelineRoleBindingForServiceAccount creates a RoleBinding for the passed serviceAccount to enable
// retrieving the necessary CRs from the passed namespace.
func (r *Controller) CreateReleasePipelineRoleBindingForServiceAccount(namespace string, serviceAccount *corev1.ServiceAccount) (*rbac.RoleBinding, error) {
	roleBinding := &rbac.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "release-service-pipeline-rolebinding-",
			Namespace:    namespace,
		},
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     "release-pipeline-resource-role",
		},
		Subjects: []rbac.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      serviceAccount.Name,
				Namespace: serviceAccount.Namespace,
			},
		},
	}
	err := r.KubeRest().Create(context.Background(), roleBinding)
	if err != nil {
		return nil, err
	}
	return roleBinding, nil
}

// GetRelease returns the release with in the given namespace.
// It can find a Release CR based on provided name or a name of an associated Snapshot
func (r *Controller) GetRelease(releaseName, snapshotName, namespace string) (*releaseApi.Release, error) {
	ctx := context.Background()
	if len(releaseName) > 0 {
		release := &releaseApi.Release{}
		err := r.KubeRest().Get(ctx, types.NamespacedName{Name: releaseName, Namespace: namespace}, release)
		if err != nil {
			return nil, fmt.Errorf("failed to get Release with name '%s' in '%s' namespace", releaseName, namespace)
		}
		return release, nil
	}
	releaseList := &releaseApi.ReleaseList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}
	if err := r.KubeRest().List(context.Background(), releaseList, opts...); err != nil {
		return nil, err
	}
	for _, r := range releaseList.Items {
		if len(snapshotName) > 0 && r.Spec.Snapshot == snapshotName {
			return &r, nil
		}
	}
	return nil, fmt.Errorf("could not find Release CR based on associated Snapshot '%s' in '%s' namespace", snapshotName, namespace)
}

// GetReleases returns the list of Release CR in the given namespace.
func (r *Controller) GetReleases(namespace string) (*releaseApi.ReleaseList, error) {
	releaseList := &releaseApi.ReleaseList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}
	err := r.KubeRest().List(context.Background(), releaseList, opts...)

	return releaseList, err
}

// StoreRelease stores a given Release as an artifact.
func (r *Controller) StoreRelease(release *releaseApi.Release) error {
	if release == nil {
		return fmt.Errorf("release CR is nil")
	}

	artifacts := make(map[string][]byte)
	releaseConditionStatus, err := r.GetReleaseConditionStatusMessages(release.Name, release.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get release condition status: %w", err)
	}
	artifacts["release-condition-status-"+release.Name+".log"] = []byte(strings.Join(releaseConditionStatus, "\n"))

	releaseYaml, err := yaml.Marshal(release)
	if err != nil {
		return fmt.Errorf("failed to marshal release YAML: %w", err)
	}
	artifacts["release-"+release.Name+".yaml"] = releaseYaml

	if err := logs.StoreArtifacts(artifacts); err != nil {
		return fmt.Errorf("failed to store artifacts: %w", err)
	}

	return nil
}

// Get the message from the status of a release. Useful for debugging purposes.
func (r *Controller) GetReleaseConditionStatusMessages(name, namespace string) (messages []string, err error) {
	release, err := r.GetRelease(name, "", namespace)
	if err != nil {
		return messages, fmt.Errorf("error getting Release: %v", err)
	}
	for _, condition := range release.Status.Conditions {
		messages = append(messages, fmt.Sprintf("condition.Type: %s, condition.Status: %s, condition.Reason: %s\n",
			condition.Type, condition.Status, condition.Reason))
	}
	return
}

// GetFirstReleaseInNamespace returns the first Release from  list of releases in the given namespace.
func (r *Controller) GetFirstReleaseInNamespace(namespace string) (*releaseApi.Release, error) {
	releaseList, err := r.GetReleases(namespace)

	if err != nil || len(releaseList.Items) < 1 {
		return nil, fmt.Errorf("could not find any Releases in namespace %s: %+v", namespace, err)
	}
	return &releaseList.Items[0], nil
}

// GetPipelineRunInNamespace returns the Release PipelineRun referencing the given release.
func (r *Controller) GetPipelineRunInNamespace(namespace, releaseName, releaseNamespace string) (*pipeline.PipelineRun, error) {
	pipelineRuns := &pipeline.PipelineRunList{}
	opts := []client.ListOption{
		client.MatchingLabels{
			"release.appstudio.openshift.io/name":      releaseName,
			"release.appstudio.openshift.io/namespace": releaseNamespace,
		},
		client.InNamespace(namespace),
	}

	err := r.KubeRest().List(context.Background(), pipelineRuns, opts...)

	if err == nil && len(pipelineRuns.Items) > 1 {
		return &pipelineRuns.Items[0], fmt.Errorf("found multiple PipelineRun in managed namespace '%s' for a release '%s' in '%s' namespace", namespace, releaseName, releaseNamespace)
	}

	if err == nil && len(pipelineRuns.Items) == 1 {
		return &pipelineRuns.Items[0], nil
	}

	if err == nil && len(pipelineRuns.Items) == 0 {
		return nil, fmt.Errorf("couldn't find PipelineRun in managed namespace '%s' for a release '%s' in '%s' namespace", namespace, releaseName, releaseNamespace)
	}

	return nil, fmt.Errorf("couldn't find PipelineRun in managed namespace '%s' for a release '%s' in '%s' namespace because of err:'%w'", namespace, releaseName, releaseNamespace, err)
}

// WaitForReleasePipelineToGetStarted wait for given release pipeline to get started.
// In case of failure, this function retries till it gets timed out.
func (r *Controller) WaitForReleasePipelineToGetStarted(release *releaseApi.Release, managedNamespace string) (*pipeline.PipelineRun, error) {
	var releasePipelinerun *pipeline.PipelineRun

	err := wait.PollUntilContextTimeout(context.Background(), time.Second*2, time.Minute*5, true, func(ctx context.Context) (done bool, err error) {
		releasePipelinerun, err = r.GetPipelineRunInNamespace(managedNamespace, release.GetName(), release.GetNamespace())
		if err != nil {
			ginkgo.GinkgoWriter.Println("PipelineRun has not been created yet for release %s/%s", release.GetNamespace(), release.GetName())
			return false, nil
		}
		if !releasePipelinerun.HasStarted() {
			ginkgo.GinkgoWriter.Println("pipelinerun %s/%s hasn't started yet", releasePipelinerun.GetNamespace(), releasePipelinerun.GetName())
			return false, nil
		}
		return true, nil
	})

	return releasePipelinerun, err
}

// WaitForReleasePipelineToBeFinished wait for given release pipeline to finish.
// It exposes the error message from the failed task to the end user when the pipelineRun failed.
func (r *Controller) WaitForReleasePipelineToBeFinished(release *releaseApi.Release, managedNamespace string) error {
	return wait.PollUntilContextTimeout(context.Background(), constants.PipelineRunPollingInterval, 30*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		pipelineRun, err := r.GetPipelineRunInNamespace(managedNamespace, release.GetName(), release.GetNamespace())
		if err != nil {
			ginkgo.GinkgoWriter.Println("PipelineRun has not been created yet for release %s/%s", release.GetNamespace(), release.GetName())
			return false, nil
		}
		for _, condition := range pipelineRun.Status.Conditions {
			ginkgo.GinkgoWriter.Printf("PipelineRun %s reason: %s\n", pipelineRun.Name, condition.Reason)

			if !pipelineRun.IsDone() {
				return false, nil
			}

			if pipelineRun.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue() {
				return true, nil
			} else {
				logs, _ := tekton.GetFailedPipelineRunLogs(r.KubeRest(), r.KubeInterface(), pipelineRun)
				return false, fmt.Errorf("%s", logs)
			}
		}
		return false, nil
	})
}

// DeleteRelease deletes a Release CR from the given namespace.
func (r *Controller) DeleteRelease(name, namespace string) error {
	release := &releaseApi.Release{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := r.KubeRest().Delete(context.Background(), release)
	if err != nil && !k8sErrors.IsNotFound(err) {
		return fmt.Errorf("delete Release %s/%s: %w", namespace, name, err)
	}
	return nil
}

// pipelineRunHasTransientFailure checks whether a failed PipelineRun failed due
// to a transient image-pull error (TaskRunImagePullFailed or truncated pull).
// It inspects both child TaskRun conditions and the PipelineRun-level condition.
func pipelineRunHasTransientFailure(pr *pipeline.PipelineRun, c client.Client) bool {
	for _, chr := range pr.Status.ChildReferences {
		taskRun := &pipeline.TaskRun{}
		key := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
		if err := c.Get(context.Background(), key, taskRun); err != nil {
			continue
		}
		for _, cond := range taskRun.Status.Conditions {
			if cond.Reason == "TaskRunImagePullFailed" {
				return true
			}
		}
	}
	// PipelineRun-level reason is often just "Failed"; fall back to message.
	cond := pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded)
	if cond == nil {
		return false
	}
	return cond.Reason == "TaskRunImagePullFailed" ||
		strings.Contains(cond.Message, "TaskRunImagePullFailed") ||
		strings.Contains(cond.Message, "unexpected EOF")
}

// WaitForReleasePipelineToBeFinishedWithRetry waits for the managed release
// PipelineRun to succeed. On transient image-pull failures it deletes the failed
// Release and creates a new one for the same snapshot/ReleasePlan, mirroring the
// build-pipeline retry logic in has.WaitForComponentPipelineToBeFinished.
//
// releaseToUpdate, when non-nil, is set to the (possibly new) Release CR so that
// downstream assertions use the correct Release name.
func (r *Controller) WaitForReleasePipelineToBeFinishedWithRetry(
	release *releaseApi.Release,
	managedNamespace string,
	retryOpts *has.RetryOptions,
	releaseToUpdate **releaseApi.Release,
) error {
	if retryOpts == nil {
		retryOpts = &has.RetryOptions{Retries: 2}
	}

	attempts := 1
	current := release

	for {
		err := wait.PollUntilContextTimeout(
			context.Background(),
			constants.PipelineRunPollingInterval,
			releasePipelineTimeout,
			true,
			func(ctx context.Context) (bool, error) {
				pr, getErr := r.GetPipelineRunInNamespace(managedNamespace, current.Name, current.Namespace)
				if getErr != nil {
					ginkgo.GinkgoWriter.Printf("waiting for release PipelineRun for %s/%s\n", current.Namespace, current.Name)
					return false, nil
				}
				if !pr.IsDone() {
					return false, nil
				}
				if tekton.HasPipelineRunSucceeded(pr) {
					return true, nil
				}
				failedLogs, _ := tekton.GetFailedPipelineRunLogs(r.KubeRest(), r.KubeInterface(), pr)
				return false, fmt.Errorf("%s", failedLogs)
			},
		)

		if err == nil {
			if releaseToUpdate != nil {
				*releaseToUpdate = current
			}
			return nil
		}

		pr, _ := r.GetPipelineRunInNamespace(managedNamespace, current.Name, current.Namespace)
		transient := pr != nil && pipelineRunHasTransientFailure(pr, r.KubeRest())
		if attempts > retryOpts.Retries || (!retryOpts.Always && !transient) {
			return err
		}

		ginkgo.GinkgoWriter.Printf(
			"attempt %d/%d: release %s/%s failed with transient error, recreating Release: %v\n",
			attempts, retryOpts.Retries, current.Namespace, current.Name, err,
		)

		oldName := current.Name
		snapshotName := current.Spec.Snapshot
		releasePlan := current.Spec.ReleasePlan
		tenantNS := current.Namespace

		if delErr := r.DeleteRelease(oldName, tenantNS); delErr != nil {
			return fmt.Errorf("delete failed Release %s/%s: %w", tenantNS, oldName, delErr)
		}

		retryName := fmt.Sprintf("%s-r%d", oldName, attempts)
		if len(retryName) > 63 {
			retryName = retryName[:63]
		}

		newRelease, createErr := r.CreateRelease(retryName, tenantNS, snapshotName, releasePlan)
		if createErr != nil {
			return fmt.Errorf("recreate Release for snapshot %q: %w", snapshotName, createErr)
		}

		current = newRelease
		attempts++
	}
}

const releasePipelineTimeout = 15 * time.Minute
