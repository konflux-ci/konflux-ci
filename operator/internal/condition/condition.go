/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package condition

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
)

// SetCondition updates or adds a condition to a resource's status.
// It automatically sets LastTransitionTime and ObservedGeneration.
// If the condition status hasn't changed, LastTransitionTime is preserved.
func SetCondition(obj konfluxv1alpha1.ConditionAccessor, condition metav1.Condition) {
	condition.ObservedGeneration = obj.GetGeneration()

	conditions := obj.GetConditions()
	apimeta.SetStatusCondition(&conditions, condition)
	obj.SetConditions(conditions)
}

// SetFailedCondition sets a failed condition with ConditionFalse status on the resource.
// This is a convenience function for error handling in reconcilers.
// The caller is responsible for persisting the status update.
func SetFailedCondition(obj konfluxv1alpha1.ConditionAccessor, conditionType string, reason string, err error) {
	SetCondition(obj, metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  reason,
		Message: err.Error(),
	})
}

// CleanupStaleConditions removes conditions that don't match the shouldKeep predicate.
// This is useful for removing conditions for resources that no longer exist.
func CleanupStaleConditions(obj konfluxv1alpha1.ConditionAccessor, shouldKeep func(cond metav1.Condition) bool) {
	var newConditions []metav1.Condition
	for _, cond := range obj.GetConditions() {
		if shouldKeep(cond) {
			newConditions = append(newConditions, cond)
		}
	}
	obj.SetConditions(newConditions)
}

// IsDeploymentReady returns true if the deployment has all replicas ready and updated.
func IsDeploymentReady(deployment *appsv1.Deployment) bool {
	return deployment.Status.ReadyReplicas == deployment.Status.Replicas &&
		deployment.Status.Replicas > 0 &&
		deployment.Status.UpdatedReplicas == deployment.Status.Replicas
}

// DeploymentCondition creates a condition representing the current state of a deployment.
// The condition type is formatted as "namespace/name".
func DeploymentCondition(deployment *appsv1.Deployment) metav1.Condition {
	conditionType := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)

	if IsDeploymentReady(deployment) {
		return metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonDeploymentReady,
			Message: fmt.Sprintf("Deployment has %d/%d replicas ready", deployment.Status.ReadyReplicas, deployment.Status.Replicas),
		}
	}

	message := fmt.Sprintf("Ready: %d/%d, Updated: %d/%d",
		deployment.Status.ReadyReplicas, deployment.Status.Replicas,
		deployment.Status.UpdatedReplicas, deployment.Status.Replicas)

	// Check for specific conditions that indicate problems
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse {
			message = fmt.Sprintf("%s - %s: %s", message, cond.Reason, cond.Message)
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			message = fmt.Sprintf("%s - ReplicaFailure: %s", message, cond.Message)
		}
	}

	return metav1.Condition{
		Type:    conditionType,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonDeploymentNotReady,
		Message: message,
	}
}

// DeploymentStatusSummary contains aggregated status information about a set of deployments.
type DeploymentStatusSummary struct {
	// AllReady is true if all deployments are ready.
	AllReady bool
	// TotalCount is the total number of deployments.
	TotalCount int
	// NotReadyNames contains the names of deployments that are not ready.
	NotReadyNames []string
	// SeenConditionTypes contains the condition types for all processed deployments.
	SeenConditionTypes map[string]bool
}

// SetDeploymentConditions sets conditions on the object for each deployment in the list
// and returns a summary of the deployment statuses.
func SetDeploymentConditions(obj konfluxv1alpha1.ConditionAccessor, deployments []appsv1.Deployment) DeploymentStatusSummary {
	summary := DeploymentStatusSummary{
		AllReady:           true,
		TotalCount:         len(deployments),
		SeenConditionTypes: make(map[string]bool),
	}

	for i := range deployments {
		deployment := &deployments[i]
		cond := DeploymentCondition(deployment)
		summary.SeenConditionTypes[cond.Type] = true

		if !IsDeploymentReady(deployment) {
			summary.AllReady = false
			summary.NotReadyNames = append(summary.NotReadyNames, deployment.Name)
		}

		SetCondition(obj, cond)
	}

	return summary
}

// SetOverallReadyCondition sets the overall Ready condition based on the deployment status summary.
func SetOverallReadyCondition(obj konfluxv1alpha1.ConditionAccessor, summary DeploymentStatusSummary) {
	if summary.AllReady {
		var message string
		if summary.TotalCount == 0 {
			message = "Component ready (no deployments to track)"
		} else {
			message = fmt.Sprintf("All %d deployments are ready", summary.TotalCount)
		}
		SetCondition(obj, metav1.Condition{
			Type:    TypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonAllComponentsReady,
			Message: message,
		})
	} else {
		SetCondition(obj, metav1.Condition{
			Type:    TypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonComponentsNotReady,
			Message: fmt.Sprintf("Deployments not ready: %v", summary.NotReadyNames),
		})
	}
}

// IsConditionTrue returns true if the specified condition type has status True.
func IsConditionTrue(obj konfluxv1alpha1.ConditionAccessor, conditionType string) bool {
	cond := apimeta.FindStatusCondition(obj.GetConditions(), conditionType)
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// SubCRStatus represents the readiness status of a sub-CR.
type SubCRStatus struct {
	// Name is the name/identifier of the sub-CR (e.g., "KonfluxBuildService")
	Name string
	// Ready indicates if the sub-CR is ready
	Ready bool
}

// AggregateReadiness computes the overall readiness from sub-CR statuses.
// Returns whether all components are ready and a slice of reasons for any not-ready components.
func AggregateReadiness(subCRStatuses []SubCRStatus) (allReady bool, notReadyReasons []string) {
	allReady = true

	for _, status := range subCRStatuses {
		if !status.Ready {
			allReady = false
			notReadyReasons = append(notReadyReasons, fmt.Sprintf("%s is not ready", status.Name))
		}
	}

	return allReady, notReadyReasons
}

// SetAggregatedReadyCondition sets the overall Ready condition based on aggregated sub-CR readiness.
// All deployments are managed by component-specific reconcilers, so we only aggregate sub-CR statuses.
func SetAggregatedReadyCondition(
	obj konfluxv1alpha1.ConditionAccessor,
	subCRStatuses []SubCRStatus,
) {
	allReady, notReadyReasons := AggregateReadiness(subCRStatuses)

	if allReady {
		// Count total components (sub-CRs)
		totalComponents := len(subCRStatuses)
		SetCondition(obj, metav1.Condition{
			Type:    TypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  ReasonAllComponentsReady,
			Message: fmt.Sprintf("All %d components are ready", totalComponents),
		})
	} else {
		SetCondition(obj, metav1.Condition{
			Type:    TypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonComponentsNotReady,
			Message: strings.Join(notReadyReasons, "; "),
		})
	}
}

// CopySubCRStatus copies conditions from a sub-CR to a parent CR with a prefix.
// It removes existing conditions with the prefix, copies new ones, and returns the sub-CR's ready status.
// The caller is responsible for fetching the sub-CR before calling this function.
func CopySubCRStatus(
	parent konfluxv1alpha1.ConditionAccessor,
	subCR konfluxv1alpha1.ConditionAccessor,
	conditionPrefix string,
) SubCRStatus {
	prefixWithDot := conditionPrefix + "."

	// Track which prefixed conditions we're updating (to detect stale ones)
	updatedConditions := make(map[string]bool)

	// Copy conditions from sub-CR to parent, prefixing with the condition prefix.
	// SetCondition (via apimeta.SetStatusCondition) handles LastTransitionTime correctly:
	// it only updates the time when the status actually changes.
	for _, cond := range subCR.GetConditions() {
		// Replace slashes with dots in the condition type to avoid multiple slashes
		sanitizedType := strings.ReplaceAll(cond.Type, "/", ".")
		// Prefix the condition type to namespace it under the sub-CR
		prefixedType := fmt.Sprintf("%s.%s", conditionPrefix, sanitizedType)
		updatedConditions[prefixedType] = true

		SetCondition(parent, metav1.Condition{
			Type:               prefixedType,
			Status:             cond.Status,
			Reason:             cond.Reason,
			Message:            cond.Message,
			ObservedGeneration: cond.ObservedGeneration,
		})
	}

	// Remove stale conditions (prefixed conditions that no longer exist in sub-CR)
	parentConditions := parent.GetConditions()
	newConditions := make([]metav1.Condition, 0, len(parentConditions))
	for _, cond := range parentConditions {
		if strings.HasPrefix(cond.Type, prefixWithDot) && !updatedConditions[cond.Type] {
			continue // Skip stale condition
		}
		newConditions = append(newConditions, cond)
	}
	parent.SetConditions(newConditions)

	return SubCRStatus{
		Name:  conditionPrefix,
		Ready: IsConditionTrue(subCR, TypeReady),
	}
}

// UpdateComponentStatuses is a generic helper that checks the status of all owned Deployments
// and updates the CR's status conditions in memory. It can be used by any controller that manages
// deployments and implements ConditionAccessor.
//
// This function only modifies the CR object in memory. The caller is responsible for persisting
// the status update to the Kubernetes API (e.g., via k8sClient.Status().Update()).
// This allows the caller to batch multiple status changes and perform a single update,
// avoiding conflicts when multiple changes occur during a reconcile loop.
//
// Parameters:
//   - ctx: The context for the operation
//   - k8sClient: The Kubernetes client for listing deployments
//   - cr: The custom resource that implements ConditionAccessor (e.g., KonfluxBuildService, KonfluxIntegrationService)
func UpdateComponentStatuses(
	ctx context.Context,
	k8sClient client.Client,
	cr konfluxv1alpha1.ConditionAccessor,
) error {
	// List all deployments owned by this CR instance
	deploymentList := &appsv1.DeploymentList{}
	if err := k8sClient.List(ctx, deploymentList, client.MatchingLabels{
		constant.KonfluxOwnerLabel: cr.GetName(),
	}); err != nil {
		return fmt.Errorf("failed to list owned deployments: %w", err)
	}

	// Set conditions for each deployment and get summary
	summary := SetDeploymentConditions(cr, deploymentList.Items)

	// Remove conditions for deployments that no longer exist
	CleanupStaleConditions(cr, func(cond metav1.Condition) bool {
		return cond.Type == TypeReady || summary.SeenConditionTypes[cond.Type]
	})

	// Set the overall Ready condition
	SetOverallReadyCondition(cr, summary)

	return nil
}

// DependencyOverride defines how a dependency condition should override Ready status.
type DependencyOverride struct {
	// ConditionType is the type of the dependency condition to check (e.g., "CertManagerAvailable").
	ConditionType string
	// Reason is the reason to use when overriding Ready to False.
	Reason string
	// Message is the message to use when overriding Ready to False.
	Message string
}

// OverrideReadyIfDependencyFalse checks if any dependency conditions are explicitly False
// and overrides Ready to False if so. Only checks conditions that are explicitly False,
// not Unknown, to allow Ready to remain True when dependencies are uncertain.
func OverrideReadyIfDependencyFalse(
	obj konfluxv1alpha1.ConditionAccessor,
	dependencies []DependencyOverride,
) {
	for _, dep := range dependencies {
		cond := apimeta.FindStatusCondition(obj.GetConditions(), dep.ConditionType)
		if cond != nil && cond.Status == metav1.ConditionFalse {
			// Dependency is explicitly missing, override Ready to False
			SetCondition(obj, metav1.Condition{
				Type:    TypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  dep.Reason,
				Message: dep.Message,
			})
			// Only override with the first False dependency found
			// (in case multiple dependencies are False, use the first one's reason/message)
			return
		}
	}
}
