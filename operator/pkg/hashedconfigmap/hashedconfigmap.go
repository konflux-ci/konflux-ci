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

// Package hashedconfigmap builds ConfigMaps with content-based hash suffixes,
// similar to how kustomize handles ConfigMaps. The name changes whenever the
// content changes, so a workload referencing it by name rolls out automatically.
//
// Superseded revisions are not deleted here. Callers apply the ConfigMap through
// the tracking client, which labels and tracks it, and the reconciler's existing
// CleanupOrphans pass removes any revision that was not applied this time round.
// That pass runs after the manifests are applied, so the workload already points
// at the new revision before the old one goes away. hashedsecret works the same way.
package hashedconfigmap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HashSuffixLength is the number of characters to use from the hash for the suffix.
const HashSuffixLength = 10

// Build creates a ConfigMap named baseName-<hash of content>, holding content under
// dataKey. TypeMeta is set so the object is ready for server-side apply.
func Build(baseName, namespace, dataKey, content string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      BuildConfigMapName(baseName, content),
			Namespace: namespace,
		},
		Data: map[string]string{
			dataKey: content,
		},
	}
}

// GenerateHashSuffix returns the hash suffix for the given content.
func GenerateHashSuffix(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])[:HashSuffixLength]
}

// BuildConfigMapName returns the full ConfigMap name for the given base name and content.
func BuildConfigMapName(baseName, content string) string {
	return fmt.Sprintf("%s-%s", baseName, GenerateHashSuffix(content))
}
