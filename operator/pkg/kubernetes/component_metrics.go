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
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	secretGVK             = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	serviceAccountGVK     = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}
	clusterRoleGVK        = rbacv1.SchemeGroupVersion.WithKind("ClusterRole")
	clusterRoleBindingGVK = rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding")
)

// ComponentMetricsOrphanCleanupGVKs lists GVKs for interim metrics scrape resources that
// may be skipped during apply or superseded across operator releases.
var ComponentMetricsOrphanCleanupGVKs = []schema.GroupVersionKind{
	serviceMonitorGVK,
	secretGVK,
	serviceAccountGVK,
	clusterRoleGVK,
	clusterRoleBindingGVK,
}

// IsComponentMetricsServiceMonitor reports whether obj is the operand ServiceMonitor
// from upstream-kustomizations/*/monitoring/. Operand reconcilers skip this object in
// applyManifests when componentMetrics is enabled (deferred ServiceMonitor apply); it is
// applied later from ReconcilePrometheusScrapeToken after prometheus-scrape-token is readable.
func IsComponentMetricsServiceMonitor(obj client.Object) bool {
	if obj == nil {
		return false
	}
	gvk := objectGroupVersionKind(obj)
	return gvk.Group == serviceMonitorGVK.Group && gvk.Kind == serviceMonitorGVK.Kind
}

// IsComponentMetricsScrapeResource reports whether obj is part of the component metrics
// scrape contract under upstream-kustomizations/*/monitoring/ (ServiceMonitor,
// metrics-reader ClusterRole, prometheus-* ClusterRoleBinding). Legacy dedicated
// metrics-reader ServiceAccounts and static token Secrets are included so upgrades
// can remove resources from older static-token scrape layouts.
func IsComponentMetricsScrapeResource(obj client.Object) bool {
	if obj == nil {
		return false
	}

	gvk := objectGroupVersionKind(obj)
	name := obj.GetName()

	switch gvk.Group {
	case serviceMonitorGVK.Group:
		return gvk.Kind == serviceMonitorGVK.Kind
	case rbacv1.SchemeGroupVersion.Group:
		switch gvk.Kind {
		case clusterRoleGVK.Kind:
			return strings.HasSuffix(name, MetricsReaderNameSuffix)
		case clusterRoleBindingGVK.Kind:
			return strings.HasPrefix(name, "prometheus-") && strings.HasSuffix(name, MetricsReaderNameSuffix)
		}
	case "":
		switch gvk.Kind {
		case serviceAccountGVK.Kind:
			return isMetricsReaderServiceAccountName(name)
		case secretGVK.Kind:
			if name == ScrapeTokenSecretName {
				return true
			}
			switch o := obj.(type) {
			case *corev1.Secret:
				if o.Type != corev1.SecretTypeServiceAccountToken {
					return false
				}
				return isMetricsReaderServiceAccountName(o.Annotations["kubernetes.io/service-account.name"])
			case *unstructured.Unstructured:
				name, _, _ := unstructured.NestedString(o.Object, "metadata", "name")
				if name == ScrapeTokenSecretName {
					return true
				}
				secretType, _, _ := unstructured.NestedString(o.Object, "type")
				saName, _, _ := unstructured.NestedString(
					o.Object, "metadata", "annotations", "kubernetes.io/service-account.name",
				)
				return secretType == string(corev1.SecretTypeServiceAccountToken) &&
					isMetricsReaderServiceAccountName(saName)
			default:
				return false
			}
		}
	}

	return false
}

func objectGroupVersionKind(obj client.Object) schema.GroupVersionKind {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if !gvk.Empty() {
		return gvk
	}
	switch o := obj.(type) {
	case *rbacv1.ClusterRole:
		return clusterRoleGVK
	case *rbacv1.ClusterRoleBinding:
		return clusterRoleBindingGVK
	case *corev1.ServiceAccount:
		return serviceAccountGVK
	case *corev1.Secret:
		return secretGVK
	case *unstructured.Unstructured:
		return o.GroupVersionKind()
	default:
		return gvk
	}
}

func isMetricsReaderServiceAccountName(name string) bool {
	return name == LegacyMetricsReaderServiceAccountName || strings.HasSuffix(name, MetricsReaderNameSuffix)
}
