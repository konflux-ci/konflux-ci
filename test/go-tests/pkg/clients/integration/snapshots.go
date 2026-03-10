package integration

import (
	"context"
	"fmt"
	"time"
	"strconv"
	"sort"

	"github.com/devfile/library/v2/pkg/util"
	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	intgteststat "github.com/konflux-ci/integration-service/pkg/integrationteststatus"
	ginkgo "github.com/onsi/ginkgo/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/apimachinery/pkg/api/meta"
	"github.com/konflux-ci/operator-toolkit/metadata"
)

var (
	// SnapshotTestsStatusAnnotation is annotation in snapshot where integration test results are stored
	SnapshotTestsStatusAnnotation = "test.appstudio.openshift.io/status"

	// AppStudioIntegrationStatusCondition is the condition for marking the AppStudio integration status of the Snapshot.
	AppStudioIntegrationStatusCondition = "AppStudioIntegrationStatus"

	// AppStudioIntegrationStatusCanceled is the reason that's set when the AppStudio tests cancel.
	AppStudioIntegrationStatusCanceled = "Canceled"

	//AppStudioTestSucceededCondition is the condition for marking if the AppStudio Tests succeeded for the Snapshot.
	AppStudioTestSucceededCondition = "AppStudioTestSucceeded"

	//LegacyTestSucceededCondition is the condition for marking if the AppStudio Tests succeeded for the Snapshot.
	LegacyTestSucceededCondition = "HACBSStudioTestSucceeded"

	// LegacyIntegrationStatusCondition is the condition for marking the AppStudio integration status of the Snapshot.
	LegacyIntegrationStatusCondition = "HACBSIntegrationStatus"

	// BuildPipelineRunStartTime contains the start time of build pipelineRun
	BuildPipelineRunStartTime = "test.appstudio.openshift.io/pipelinerunstarttime"
)

// CreateSnapshotWithComponents creates a Snapshot using the given parameters.
func (i *Controller) CreateSnapshotWithComponents(snapshotName, componentName, applicationName, namespace string, snapshotComponents []appstudioApi.SnapshotComponent) (*appstudioApi.Snapshot, error) {
	snapshot := &appstudioApi.Snapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: namespace,
			Labels: map[string]string{
				"test.appstudio.openshift.io/type":           "component",
				"appstudio.openshift.io/component":           componentName,
				"pac.test.appstudio.openshift.io/event-type": "push",
			},
		},
		Spec: appstudioApi.SnapshotSpec{
			Application: applicationName,
			Components:  snapshotComponents,
		},
	}
	return snapshot, i.KubeRest().Create(context.Background(), snapshot)
}

// CreateSnapshotWithImage creates a snapshot using an image.
func (i *Controller) CreateSnapshotWithImage(componentName, applicationName, namespace, containerImage string) (*appstudioApi.Snapshot, error) {
	snapshotComponents := []appstudioApi.SnapshotComponent{
		{
			Name:           componentName,
			ContainerImage: containerImage,
		},
	}

	snapshotName := "snapshot-sample-" + util.GenerateRandomString(4)

	return i.CreateSnapshotWithComponents(snapshotName, componentName, applicationName, namespace, snapshotComponents)
}

// GetSnapshotByComponent returns the first snapshot in namespace if exist, else will return nil
func (i *Controller) GetSnapshotByComponent(namespace string) (*appstudioApi.Snapshot, error) {
	snapshot := &appstudioApi.SnapshotList{}
	opts := []client.ListOption{
		client.MatchingLabels{
			"test.appstudio.openshift.io/type": "component",
		},
		client.InNamespace(namespace),
	}
	err := i.KubeRest().List(context.Background(), snapshot, opts...)

	if err == nil && len(snapshot.Items) > 0 {
		return &snapshot.Items[0], nil
	}
	return nil, err
}

// GetSnapshot returns the Snapshot in the namespace and nil if it's not found
// It will search for the Snapshot based on the Snapshot name, associated PipelineRun name or Component name
// In the case the List operation fails, an error will be returned.
func (i *Controller) GetSnapshot(snapshotName, pipelineRunName, componentName, namespace string) (*appstudioApi.Snapshot, error) {
	ctx := context.Background()
	// If Snapshot name is provided, try to get the resource directly
	if len(snapshotName) > 0 {
		snapshot := &appstudioApi.Snapshot{}
		if err := i.KubeRest().Get(ctx, types.NamespacedName{Name: snapshotName, Namespace: namespace}, snapshot); err != nil {
			return nil, fmt.Errorf("couldn't find Snapshot with name '%s' in '%s' namespace", snapshotName, namespace)
		}
		return snapshot, nil
	}
	// Search for the Snapshot in the namespace based on the associated Component or PipelineRun
	snapshots := &appstudioApi.SnapshotList{}
	opts := []client.ListOption{
		client.InNamespace(namespace),
	}
	err := i.KubeRest().List(ctx, snapshots, opts...)
	if err != nil {
		return nil, fmt.Errorf("error when listing Snapshots in '%s' namespace", namespace)
	}
	for _, snapshot := range snapshots.Items {
		if snapshot.Name == snapshotName {
			return &snapshot, nil
		}
		// find snapshot by pipelinerun name
		if len(pipelineRunName) > 0 && snapshot.Labels["appstudio.openshift.io/build-pipelinerun"] == pipelineRunName {
			return &snapshot, nil

		}
		// find snapshot by component name
		if len(componentName) > 0 && snapshot.Labels["appstudio.openshift.io/component"] == componentName {
			return &snapshot, nil

		}
	}
	return nil, fmt.Errorf("no snapshot found for component '%s', pipelineRun '%s' in '%s' namespace", componentName, pipelineRunName, namespace)
}

// DeleteSnapshot removes given snapshot from specified namespace.
func (i *Controller) DeleteSnapshot(hasSnapshot *appstudioApi.Snapshot, namespace string) error {
	err := i.KubeRest().Delete(context.Background(), hasSnapshot)
	return err
}

// PatchSnapshot patches the given snapshot with the provided patch.
func (i *Controller) PatchSnapshot(oldSnapshot *appstudioApi.Snapshot, newSnapshot *appstudioApi.Snapshot) error {
	patch := client.MergeFrom(oldSnapshot)
	err := i.KubeRest().Patch(context.Background(), newSnapshot, patch)
	return err
}

// DeleteAllSnapshotsInASpecificNamespace removes all snapshots from a specific namespace. Useful when creating a lot of resources and want to remove all of them
func (i *Controller) DeleteAllSnapshotsInASpecificNamespace(namespace string, timeout time.Duration) error {
	if err := i.KubeRest().DeleteAllOf(context.Background(), &appstudioApi.Snapshot{}, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting snapshots from the namespace %s: %+v", namespace, err)
	}

	return utils.WaitUntil(func() (done bool, err error) {
		snapshotList, err := i.ListAllSnapshots(namespace)
		if err != nil {
			return false, nil
		}
		return len(snapshotList.Items) == 0, nil
	}, timeout)
}

// WaitForSnapshotToGetCreated wait for the Snapshot to get created successfully.
func (i *Controller) WaitForSnapshotToGetCreated(snapshotName, pipelinerunName, componentName, testNamespace string) (*appstudioApi.Snapshot, error) {
	var snapshot *appstudioApi.Snapshot

	err := wait.PollUntilContextTimeout(context.Background(), constants.PipelineRunPollingInterval, 10*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		snapshot, err = i.GetSnapshot(snapshotName, pipelinerunName, componentName, testNamespace)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("unable to get the Snapshot within the namespace %s. Error: %v", testNamespace, err)
			return false, nil
		}

		return true, nil
	})

	return snapshot, err
}

// ListAllSnapshots returns a list of all Snapshots in a given namespace.
func (i *Controller) ListAllSnapshots(namespace string) (*appstudioApi.SnapshotList, error) {
	snapshotList := &appstudioApi.SnapshotList{}
	err := i.KubeRest().List(context.Background(), snapshotList, &client.ListOptions{Namespace: namespace})

	return snapshotList, err
}

// StoreSnapshot stores a given Snapshot as an artifact.
func (i *Controller) StoreSnapshot(snapshot *appstudioApi.Snapshot) error {
	return logs.StoreResourceYaml(snapshot, "snapshot-"+snapshot.Name)
}

// StoreAllSnapshots stores all Snapshots in a given namespace.
func (i *Controller) StoreAllSnapshots(namespace string) error {
	snapshotList, err := i.ListAllSnapshots(namespace)
	if err != nil {
		return err
	}

	for _, snapshot := range snapshotList.Items {
		if err := i.StoreSnapshot(&snapshot); err != nil {
			return err
		}
	}
	return nil
}

// GetIntegrationTestStatusDetailFromSnapshot parses snapshot annotation and returns integration test status detail
func (i *Controller) GetIntegrationTestStatusDetailFromSnapshot(snapshot *appstudioApi.Snapshot, scenarioName string) (*intgteststat.IntegrationTestStatusDetail, error) {
	var (
		resultsJson string
		ok          bool
	)
	annotations := snapshot.GetAnnotations()
	resultsJson, ok = annotations[SnapshotTestsStatusAnnotation]
	if !ok {
		resultsJson = ""
	}
	statuses, err := intgteststat.NewSnapshotIntegrationTestStatuses(resultsJson)
	if err != nil {
		return nil, fmt.Errorf("failed to create new SnapshotIntegrationTestStatuses object: %w", err)
	}
	statusDetail, ok := statuses.GetScenarioStatus(scenarioName)
	if !ok {
		return nil, fmt.Errorf("status detail for scenario %s not found", scenarioName)
	}
	return statusDetail, nil
}

// IsSnapshotMarkedAsCanceled returns true if snapshot is marked as AppStudioIntegrationStatusCanceled
func (i *Controller) IsSnapshotMarkedAsCanceled(snapshot *appstudioApi.Snapshot) bool {
	return i.IsSnapshotStatusConditionSet(snapshot, AppStudioIntegrationStatusCondition, metav1.ConditionTrue, AppStudioIntegrationStatusCanceled)
}

// IsSnapshotStatusConditionSet checks if the condition with the conditionType in the status of Snapshot has been marked as the conditionStatus and reason.
func (i *Controller) IsSnapshotStatusConditionSet(snapshot *appstudioApi.Snapshot, conditionType string, conditionStatus metav1.ConditionStatus, reason string) bool {
	condition := meta.FindStatusCondition(snapshot.Status.Conditions, conditionType)
	if condition == nil && conditionType == AppStudioTestSucceededCondition {
		condition = meta.FindStatusCondition(snapshot.Status.Conditions, LegacyTestSucceededCondition)
	}
	if condition == nil && conditionType == AppStudioIntegrationStatusCondition {
		condition = meta.FindStatusCondition(snapshot.Status.Conditions, LegacyIntegrationStatusCondition)
	}
	if condition == nil || condition.Status != conditionStatus {
		return false
	}
	if reason != "" && reason != condition.Reason {
		return false
	}
	return true
}

// SortSnapshots sorts the snapshots according to the snapshot annotation BuildPipelineRunStartTime
func (i *Controller) SortSnapshots(snapshots []appstudioApi.Snapshot) []appstudioApi.Snapshot {
	sort.Slice(snapshots, func(i, j int) bool {
		// sorting snapshots according to the annotation BuildPipelineRunStartTime which
		// represents the start time of build PLR
		// when BuildPipelineRunStartTime is not set, we use its creation time
		var time_i, time_j int
		if metadata.HasAnnotation(&snapshots[i], BuildPipelineRunStartTime) && metadata.HasAnnotation(&snapshots[j], BuildPipelineRunStartTime) {
			time_i, _ = strconv.Atoi(snapshots[i].Annotations[BuildPipelineRunStartTime])
			time_j, _ = strconv.Atoi(snapshots[j].Annotations[BuildPipelineRunStartTime])
		} else {
			time_i = int(snapshots[i].CreationTimestamp.Unix())
			time_j = int(snapshots[j].CreationTimestamp.Unix())
		}
		return time_i > time_j
	})
	return snapshots
}

func (i *Controller) IsOlderSnapshotAndIntegrationPlrCancelled(snapshots []appstudioApi.Snapshot, integrationTestScenarioName string) (bool, error) {
	snapshots = i.SortSnapshots(snapshots)
	if len(snapshots) < 2 {
		return false, fmt.Errorf("the length of snapshots < 2,  there is no older snapshot")
	}

	if !i.IsSnapshotMarkedAsCanceled(&snapshots[1]) {
		return false, fmt.Errorf("older snapshot has not been cancelled as expected")
	}
	isCancelled, err := i.IsIntegrationPipelinerunCancelled(integrationTestScenarioName, &snapshots[1])
	if err != nil {
		return false, err
	}
	if !isCancelled {
		return false, fmt.Errorf("integration pipelinerun has not been cancelled as expected")
	}
	return true, nil
}
