package integration

import (
	"context"
	"fmt"
	"strings"

	integrationv1beta2 "github.com/konflux-ci/integration-service/api/v1beta2"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetRelatedResolutionRequests returns a list of ResolutionRequest objects related to the given IntegrationTestScenario
// in the specified namespace. Returns nil if no related ResolutionRequests are found, or an error if the operation fails.
func (i *IntegrationController) GetRelatedResolutionRequests(namespace string, integrationTestScenario *integrationv1beta2.IntegrationTestScenario) ([]unstructured.Unstructured, error) {
	// List all ResolutionRequest objects in the namespace using unstructured
	resolutionRequestList := &unstructured.UnstructuredList{}
	resolutionRequestList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "resolution.tekton.dev",
		Version: "v1beta1",
		Kind:    "ResolutionRequestList",
	})

	err := i.KubeRest().List(context.Background(), resolutionRequestList, client.InNamespace(namespace))
	if err != nil {
		// Check if the error is due to CRD not existing
		if meta.IsNoMatchError(err) || strings.Contains(err.Error(), "no matches for kind") {
			return nil, fmt.Errorf("ResolutionRequest CRD not available in cluster: %w", err)
		}
		return nil, fmt.Errorf("failed to list ResolutionRequests in namespace %s: %w", namespace, err)
	}

	// Filter ResolutionRequests related to the integration test scenario
	var relatedResolutionRequests []unstructured.Unstructured

	for _, rr := range resolutionRequestList.Items {
		if i.isResolutionRequestRelated(rr, integrationTestScenario) {
			relatedResolutionRequests = append(relatedResolutionRequests, rr)
		}
	}

	// Return nil if no related ResolutionRequests found
	if len(relatedResolutionRequests) == 0 {
		return nil, nil
	}

	return relatedResolutionRequests, nil
}

// isResolutionRequestRelated checks if a ResolutionRequest is related to the given IntegrationTestScenario
func (i *IntegrationController) isResolutionRequestRelated(rr unstructured.Unstructured, integrationTestScenario *integrationv1beta2.IntegrationTestScenario) bool {
	labels := rr.GetLabels()
	annotations := rr.GetAnnotations()

	// Check labels for integration test scenario relationship
	if labels != nil {
		// Direct scenario name match
		if scenarioName, exists := labels["test.appstudio.openshift.io/scenario"]; exists && scenarioName == integrationTestScenario.Name {
			return true
		}

		// Application name match
		if appName, exists := labels["appstudio.openshift.io/application"]; exists && appName == integrationTestScenario.Spec.Application {
			return true
		}

		// Pipeline type for integration tests
		if pipelineType, exists := labels["pipelines.appstudio.openshift.io/type"]; exists && pipelineType == "test" {
			return true
		}
	}

	// Check annotations for integration test scenario relationship
	if annotations != nil {
		// Check for scenario annotation
		if scenarioAnnotation, exists := annotations["test.appstudio.openshift.io/scenario"]; exists && scenarioAnnotation == integrationTestScenario.Name {
			return true
		}

		// Check for pipeline reference that matches the integration test scenario
		if pipelineRef, exists := annotations["tekton.dev/pipeline-ref"]; exists {
			// Check if the pipeline reference contains elements from the integration test scenario
			if strings.Contains(pipelineRef, integrationTestScenario.Spec.ResolverRef.Params[0].Value) || // URL
				strings.Contains(pipelineRef, integrationTestScenario.Spec.ResolverRef.Params[1].Value) || // revision
				strings.Contains(pipelineRef, integrationTestScenario.Spec.ResolverRef.Params[2].Value) { // pathInRepo
				return true
			}
		}
	}

	// Check owner references
	ownerRefs := rr.GetOwnerReferences()
	for _, ownerRef := range ownerRefs {
		// Check if owned by a PipelineRun that might be related to integration testing
		if ownerRef.Kind == "PipelineRun" &&
			(strings.Contains(ownerRef.Name, integrationTestScenario.Name) ||
				strings.Contains(ownerRef.Name, integrationTestScenario.Spec.Application)) {
			return true
		}
	}

	return false
}

// GetResolutionRequestNames returns a slice of names of the given ResolutionRequest objects
func (i *IntegrationController) GetResolutionRequestNames(resolutionRequests []unstructured.Unstructured) []string {
	names := make([]string, len(resolutionRequests))
	for i, rr := range resolutionRequests {
		names[i] = rr.GetName()
	}
	return names
}

// // WaitForResolutionRequestsCleanup waits for all ResolutionRequests related to the integration test scenario to be deleted
// func (i *IntegrationController) WaitForResolutionRequestsCleanup(namespace string, integrationTestScenario *integrationv1beta2.IntegrationTestScenario, timeoutSeconds int) error {
// 	return i.WaitForCondition(func() (bool, error) {
// 		relatedRRs, err := i.GetRelatedResolutionRequests(namespace, integrationTestScenario)
// 		if err != nil {
// 			return false, err
// 		}

// 		// Return true if no related ResolutionRequests found (cleanup complete)
// 		return relatedRRs == nil, nil
// 	}, timeoutSeconds)
// }
