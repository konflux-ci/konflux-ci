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

// ComponentMetricsOrphanCleanupGVKs lists GVKs for interim metrics scrape resources that
// may be skipped during apply or superseded across operator releases.
var ComponentMetricsOrphanCleanupGVKs = []schema.GroupVersionKind{
	{Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor"},
	{Group: "", Version: "v1", Kind: "Secret"},
	{Group: "", Version: "v1", Kind: "ServiceAccount"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"},
	{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRoleBinding"},
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
	return gvk.Group == "monitoring.coreos.com" && gvk.Kind == "ServiceMonitor"
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
	case "monitoring.coreos.com":
		return gvk.Kind == "ServiceMonitor"
	case rbacv1.SchemeGroupVersion.Group:
		switch gvk.Kind {
		case "ClusterRole":
			return strings.HasSuffix(name, MetricsReaderNameSuffix)
		case "ClusterRoleBinding":
			return strings.HasPrefix(name, "prometheus-") && strings.HasSuffix(name, MetricsReaderNameSuffix)
		}
	case "":
		switch gvk.Kind {
		case "ServiceAccount":
			return isMetricsReaderServiceAccountName(name)
		case "Secret":
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
		return rbacv1.SchemeGroupVersion.WithKind("ClusterRole")
	case *rbacv1.ClusterRoleBinding:
		return rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding")
	case *corev1.ServiceAccount:
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ServiceAccount"}
	case *corev1.Secret:
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	case *unstructured.Unstructured:
		return o.GroupVersionKind()
	default:
		return gvk
	}
}

func isMetricsReaderServiceAccountName(name string) bool {
	return name == LegacyMetricsReaderServiceAccountName || strings.HasSuffix(name, MetricsReaderNameSuffix)
}
