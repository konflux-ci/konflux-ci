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

package operatormetrics

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

const operatorServiceMonitorName = "controller-manager-metrics-monitor"

var serviceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

func desiredOperatorServiceMonitor(namespace string) *unstructured.Unstructured {
	sm := &unstructured.Unstructured{Object: map[string]interface{}{}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetName(operatorServiceMonitorName)
	sm.SetNamespace(namespace)
	sm.SetLabels(map[string]string{
		"control-plane":                "controller-manager",
		"app.kubernetes.io/name":       OperatorAppName,
		"app.kubernetes.io/managed-by": OperatorAppName,
	})
	sm.Object["spec"] = map[string]interface{}{
		"endpoints": []interface{}{
			map[string]interface{}{
				"bearerTokenSecret": map[string]interface{}{
					"key":  kubernetes.ScrapeTokenSecretKey,
					"name": kubernetes.ScrapeTokenSecretName,
				},
				"path":   "/metrics",
				"port":   "https",
				"scheme": "https",
				"tlsConfig": map[string]interface{}{
					"insecureSkipVerify": true,
				},
			},
		},
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"control-plane":          "controller-manager",
				"app.kubernetes.io/name": OperatorAppName,
			},
		},
	}
	return sm
}

// EnsureOperatorServiceMonitor creates or updates the operator metrics ServiceMonitor.
// When the ServiceMonitor CRD is not installed, reconciliation is skipped without error.
func EnsureOperatorServiceMonitor(ctx context.Context, c client.Client, namespace string) error {
	if namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	desired := desiredOperatorServiceMonitor(namespace)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(serviceMonitorGVK)

	err := c.Get(ctx, client.ObjectKey{
		Name:      operatorServiceMonitorName,
		Namespace: namespace,
	}, existing)
	if meta.IsNoMatchError(err) {
		return nil
	}
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			if meta.IsNoMatchError(err) {
				return nil
			}
			return fmt.Errorf("create ServiceMonitor %q: %w", operatorServiceMonitorName, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ServiceMonitor %q: %w", operatorServiceMonitorName, err)
	}

	patch := existing.DeepCopy()
	patch.SetLabels(desired.GetLabels())
	desiredSpec, _, _ := unstructured.NestedMap(desired.Object, "spec")
	if err := unstructured.SetNestedMap(patch.Object, desiredSpec, "spec"); err != nil {
		return fmt.Errorf("set ServiceMonitor spec: %w", err)
	}
	if patch.GetAnnotations() == nil {
		patch.SetAnnotations(map[string]string{})
	}
	return c.Patch(ctx, patch, client.MergeFrom(existing))
}
