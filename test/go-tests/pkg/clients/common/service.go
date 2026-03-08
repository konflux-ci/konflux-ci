package common

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// GetServiceByName returns the service for a given component name
func (h *SuiteController) GetServiceByName(serviceName string, serviceNamespace string) (*corev1.Service, error) {
	namespacedName := types.NamespacedName{
		Name:      serviceName,
		Namespace: serviceNamespace,
	}

	service := &corev1.Service{}
	err := h.KubeRest().Get(context.Background(), namespacedName, service)
	if err != nil {
		return &corev1.Service{}, err
	}
	return service, nil
}
