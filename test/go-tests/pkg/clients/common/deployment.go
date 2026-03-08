package common

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

// GetAppDeploymentByName returns the deployment for a given component name
func (h *SuiteController) GetDeployment(deploymentName string, namespace string) (*appsv1.Deployment, error) {
	namespacedName := types.NamespacedName{
		Name:      deploymentName,
		Namespace: namespace,
	}

	deployment := &appsv1.Deployment{}
	err := h.KubeRest().Get(context.Background(), namespacedName, deployment)
	if err != nil {
		return &appsv1.Deployment{}, err
	}
	return deployment, nil
}

// Checks and waits for a kubernetes deployment object to be completed or not
func (h *SuiteController) DeploymentIsCompleted(deploymentName, namespace string, readyReplicas int32) wait.ConditionFunc {
	return func() (bool, error) {
		namespacedName := types.NamespacedName{
			Name:      deploymentName,
			Namespace: namespace,
		}

		deployment := &appsv1.Deployment{}
		err := h.KubeRest().Get(context.Background(), namespacedName, deployment)
		if err != nil && !k8sErrors.IsNotFound(err) {
			return false, err
		}
		if deployment.Status.AvailableReplicas == readyReplicas && deployment.Status.UnavailableReplicas == 0 {
			return true, nil
		}
		return false, nil
	}
}
