package common

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Create and return a configmap by cm name and namespace from the cluster
func (s *SuiteController) CreateConfigMap(cm *corev1.ConfigMap, namespace string) (*corev1.ConfigMap, error) {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Create(context.Background(), cm, metav1.CreateOptions{})
}

// Update and return a configmap by configmap cm name and namespace from the cluster
func (s *SuiteController) UpdateConfigMap(cm *corev1.ConfigMap, namespace string) (*corev1.ConfigMap, error) {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Update(context.Background(), cm, metav1.UpdateOptions{})
}

// Get a configmap by name and namespace from the cluster
func (s *SuiteController) GetConfigMap(name, namespace string) (*corev1.ConfigMap, error) {
	return s.KubeInterface().CoreV1().ConfigMaps(namespace).Get(context.Background(), name, metav1.GetOptions{})
}

// DeleteConfigMaps delete a ConfigMap. Optionally, it can avoid returning an error if the resource did not exist:
// - specify 'false' if it's likely the ConfigMap has already been deleted (for example, because the Namespace was deleted)
func (s *SuiteController) DeleteConfigMap(name, namespace string, returnErrorOnNotFound bool) error {
	err := s.KubeInterface().CoreV1().ConfigMaps(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if err != nil && k8sErrors.IsNotFound(err) && !returnErrorOnNotFound {
		err = nil // Ignore not found errors, if requested
	}
	return err
}
