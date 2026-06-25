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

package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/gomega"
)

func TestIsComponentMetricsScrapeResource(t *testing.T) {
	g := NewWithT(t)

	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "ServiceMonitor",
			"metadata": map[string]interface{}{
				"name":      "build-service",
				"namespace": "build-service",
			},
		},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "build-service-metrics-reader"},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "build-service-metrics-auth-role"},
	})).To(BeFalse())

	g.Expect(IsComponentMetricsScrapeResource(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "prometheus-build-service-metrics-reader"},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "integration-service-metrics-reader", Namespace: "integration-service"},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "metrics-reader",
			Namespace: "build-service",
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": "metrics-reader",
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ScrapeTokenSecretName,
			Namespace: "build-service",
		},
		Type: corev1.SecretTypeOpaque,
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "build-pipeline-config", Namespace: "build-service"},
	})).To(BeFalse())

	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      ScrapeTokenSecretName,
				"namespace": "build-service",
			},
		},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"type":       string(corev1.SecretTypeServiceAccountToken),
			"metadata": map[string]interface{}{
				"name":      "metrics-reader-token",
				"namespace": "build-service",
				"annotations": map[string]interface{}{
					"kubernetes.io/service-account.name": "build-service-metrics-reader",
				},
			},
		},
	})).To(BeTrue())

	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"type":       string(corev1.SecretTypeServiceAccountToken),
			"metadata": map[string]interface{}{
				"name":      "other",
				"namespace": "build-service",
			},
		},
	})).To(BeFalse())

	g.Expect(IsComponentMetricsScrapeResource(nil)).To(BeFalse())

	g.Expect(ComponentMetricsOrphanCleanupGVKs).To(ContainElement(
		schema.GroupVersionKind{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
	))
}

func TestIsComponentMetricsScrapeResource_TypedSecretBranches(t *testing.T) {
	g := NewWithT(t)

	g.Expect(IsComponentMetricsScrapeResource(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "metrics-reader-token", Namespace: "build-service"},
		Type:       corev1.SecretTypeOpaque,
	})).To(BeFalse())

	g.Expect(IsComponentMetricsScrapeResource(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "metrics-reader-token",
			Namespace: "build-service",
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": "build-service-metrics-reader",
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	})).To(BeTrue())
}

func TestIsComponentMetricsScrapeResource_UnstructuredSecretNameMatch(t *testing.T) {
	g := NewWithT(t)
	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      ScrapeTokenSecretName,
				"namespace": "image-controller",
			},
		},
	})).To(BeTrue())
}

func TestIsComponentMetricsScrapeResource_UnknownObject(t *testing.T) {
	g := NewWithT(t)
	g.Expect(IsComponentMetricsScrapeResource(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "other-binding"},
	})).To(BeFalse())
}

func TestIsComponentMetricsScrapeResource_UnstructuredServiceAccount(t *testing.T) {
	g := NewWithT(t)
	g.Expect(IsComponentMetricsScrapeResource(&unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]interface{}{
				"name":      "integration-service-metrics-reader",
				"namespace": "integration-service",
			},
		},
	})).To(BeTrue())
}
