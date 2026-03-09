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

// Package hashedsecret provides a utility for building Kubernetes Secrets with
// content-based hash suffixes, similar to kustomize. When the Secret is
// referenced by a Deployment volume, a name change triggers a rollout.
package hashedsecret

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/contenthash"
)

// Build creates a Secret with a content-based hash suffix appended to baseName.
// The hash is derived from the data map (sorted by key for determinism), so the
// Secret name changes whenever the data changes — triggering a deployment rollout
// when used as a volume reference.
//
// The returned Secret uses StringData and has TypeMeta set for server-side apply.
func Build(baseName, namespace string, data map[string]string) *corev1.Secret {
	name := fmt.Sprintf("%s-%s", baseName, contenthash.Map(data))

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		StringData: data,
	}
}
