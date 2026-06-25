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

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MetricsScraperServiceAccountName is the operand-namespace ServiceAccount the operator
	// binds in metrics-reader ClusterRoleBindings and mints scrape tokens for.
	MetricsScraperServiceAccountName = "metrics-scraper"
	// LegacyMetricsReaderServiceAccountName is the static SA name from pre-operator scrape layouts.
	LegacyMetricsReaderServiceAccountName = "metrics-reader"
	// MetricsReaderNameSuffix matches ClusterRole and legacy ServiceAccount names (e.g. build-service-metrics-reader).
	MetricsReaderNameSuffix = "-metrics-reader"
)

// OperandMetricsScraperSA returns the operator-owned scrape identity in an operand namespace.
func OperandMetricsScraperSA(namespace string) types.NamespacedName {
	return types.NamespacedName{
		Namespace: namespace,
		Name:      MetricsScraperServiceAccountName,
	}
}

// MetricsScraperBindingSubjects returns CRB subjects for the operand metrics scraper SA.
func MetricsScraperBindingSubjects(operandNamespace string) []rbacv1.Subject {
	sa := OperandMetricsScraperSA(operandNamespace)
	return []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      sa.Name,
		Namespace: sa.Namespace,
	}}
}

// EnsureMetricsScraperServiceAccount creates the operand metrics scraper SA when absent.
func EnsureMetricsScraperServiceAccount(ctx context.Context, c client.Client, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}
	key := types.NamespacedName{Namespace: namespace, Name: MetricsScraperServiceAccountName}
	existing := &corev1.ServiceAccount{}
	err := c.Get(ctx, key, existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get metrics scraper serviceaccount %s: %w", key, err)
	}
	return c.Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MetricsScraperServiceAccountName,
			Namespace: namespace,
		},
	})
}
