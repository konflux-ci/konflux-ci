package common

import (
	"context"

	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (s *SuiteController) HaveTestsSucceeded(snapshot *appstudioApi.Snapshot) bool {
	return meta.IsStatusConditionTrue(snapshot.Status.Conditions, "HACBSTestSucceeded") ||
		meta.IsStatusConditionTrue(snapshot.Status.Conditions, "AppStudioTestSucceeded")
}

func (s *SuiteController) HaveTestsFinished(snapshot *appstudioApi.Snapshot) bool {
	return meta.FindStatusCondition(snapshot.Status.Conditions, "HACBSTestSucceeded") != nil ||
		meta.FindStatusCondition(snapshot.Status.Conditions, "AppStudioTestSucceeded") != nil
}

func (s *SuiteController) MarkTestsSucceeded(snapshot *appstudioApi.Snapshot) (*appstudioApi.Snapshot, error) {
	patch := client.MergeFrom(snapshot.DeepCopy())
	meta.SetStatusCondition(&snapshot.Status.Conditions, metav1.Condition{
		Type:    "AppStudioTestSucceeded",
		Status:  metav1.ConditionTrue,
		Reason:  "Passed",
		Message: "Snapshot Passed",
	})
	err := s.KubeRest().Status().Patch(context.Background(), snapshot, patch)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}
