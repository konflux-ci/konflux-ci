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

package common

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// TrustedCAConfigMapName is the name of the ConfigMap that OpenShift's
	// cluster network operator populates with the cluster-wide trusted CA bundle.
	TrustedCAConfigMapName = "trusted-ca"

	// OpenShiftInjectTrustedCABundleLabel is the label that triggers OpenShift's
	// CA bundle injection into a ConfigMap.
	OpenShiftInjectTrustedCABundleLabel = "config.openshift.io/inject-trusted-cabundle"
)

// EnsureTrustedCAConfigMap creates or updates the trusted-ca ConfigMap with the
// OpenShift CA injection label in the given namespace. On non-OpenShift clusters
// (or when clusterInfo is nil) this is a no-op. OpenShift's cluster network operator
// automatically populates the ca-bundle.crt key when this label is present.
func EnsureTrustedCAConfigMap(ctx context.Context, namespace string, tc *tracking.Client, ci *clusterinfo.Info) error {
	if ci == nil || !ci.IsOpenShift() {
		return nil
	}
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      TrustedCAConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				OpenShiftInjectTrustedCABundleLabel: "true",
			},
		},
	}
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply trusted-ca ConfigMap in %s: %w", namespace, err)
	}
	return nil
}
