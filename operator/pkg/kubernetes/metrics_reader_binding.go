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
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MetricsScraperBindingAnnotation marks ClusterRoleBindings whose subjects are
	// reconciled to the operator-owned metrics-scraper ServiceAccount in the operand namespace.
	MetricsScraperBindingAnnotation = "konflux.konflux-ci.dev/metrics-scraper-binding"
	metricsScraperBindingEnabled    = "true"
)

// HasMetricsScraperBindingAnnotation reports whether obj uses reconciled scraper subjects.
func HasMetricsScraperBindingAnnotation(obj metav1.Object) bool {
	if obj == nil {
		return false
	}
	return obj.GetAnnotations()[MetricsScraperBindingAnnotation] == metricsScraperBindingEnabled
}

// ApplyMetricsScraperBindingSubjects updates a ClusterRoleBinding object with scraper subjects.
func ApplyMetricsScraperBindingSubjects(obj client.Object, subjects []rbacv1.Subject) error {
	switch o := obj.(type) {
	case *rbacv1.ClusterRoleBinding:
		o.Subjects = subjects
		return nil
	case *unstructured.Unstructured:
		subjectMaps := make([]interface{}, 0, len(subjects))
		for _, subject := range subjects {
			subjectMaps = append(subjectMaps, map[string]interface{}{
				"kind":      subject.Kind,
				"name":      subject.Name,
				"namespace": subject.Namespace,
			})
		}
		if err := unstructured.SetNestedSlice(o.Object, subjectMaps, "subjects"); err != nil {
			return fmt.Errorf("set subjects: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported ClusterRoleBinding type %T", obj)
	}
}

// EnsureMetricsReaderBinding creates or updates a ClusterRoleBinding for a metrics-reader role.
func EnsureMetricsReaderBinding(
	ctx context.Context,
	c client.Client,
	bindingName, clusterRoleName string,
	subjects []rbacv1.Subject,
) error {
	if bindingName == "" || clusterRoleName == "" {
		return fmt.Errorf("binding and cluster role names are required")
	}
	if len(subjects) == 0 {
		return fmt.Errorf("at least one subject is required")
	}

	existing := &rbacv1.ClusterRoleBinding{}
	err := c.Get(ctx, client.ObjectKey{Name: bindingName}, existing)
	if apierrors.IsNotFound(err) {
		return c.Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: bindingName,
				Annotations: map[string]string{
					MetricsScraperBindingAnnotation: metricsScraperBindingEnabled,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     clusterRoleName,
			},
			Subjects: subjects,
		})
	}
	if err != nil {
		return fmt.Errorf("get ClusterRoleBinding %q: %w", bindingName, err)
	}

	patch := existing.DeepCopy()
	patch.Subjects = subjects
	if patch.Annotations == nil {
		patch.Annotations = map[string]string{}
	}
	patch.Annotations[MetricsScraperBindingAnnotation] = metricsScraperBindingEnabled
	return c.Patch(ctx, patch, client.MergeFrom(existing))
}
