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

// Package hashedconfigmap provides utilities for managing ConfigMaps with content-based
// hash suffixes, similar to how kustomize handles ConfigMaps. This approach ensures that
// pods referencing ConfigMaps are automatically restarted when the ConfigMap content changes,
// as the ConfigMap name (and thus the volume reference) changes.
package hashedconfigmap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// HashSuffixLength is the number of characters to use from the hash for the suffix.
const HashSuffixLength = 10

// HashedConfigMap handles ConfigMaps with content-based hash suffixes.
// It creates new ConfigMaps when content changes (with new hash suffixes) and
// cleans up old ConfigMaps that are no longer in use.
type HashedConfigMap struct {
	client       client.Client
	scheme       *runtime.Scheme
	baseName     string
	namespace    string
	dataKey      string
	label        string
	fieldManager string
}

// New creates a new HashedConfigMap handler.
//
// Parameters:
//   - c: The Kubernetes client for API operations
//   - scheme: The runtime scheme for setting owner references
//   - baseName: The base name for the ConfigMap (hash will be appended as suffix)
//   - namespace: The namespace where ConfigMaps will be created
//   - dataKey: The key to use in the ConfigMap's data field
//   - label: The label key to mark managed ConfigMaps (value will be "true")
//   - fieldManager: The field manager name for server-side apply operations
func New(
	c client.Client, scheme *runtime.Scheme, baseName, namespace, dataKey, label, fieldManager string,
) *HashedConfigMap {
	return &HashedConfigMap{
		client:       c,
		scheme:       scheme,
		baseName:     baseName,
		namespace:    namespace,
		dataKey:      dataKey,
		label:        label,
		fieldManager: fieldManager,
	}
}

// Result contains the result of an Apply operation.
type Result struct {
	// ConfigMapName is the full name of the ConfigMap (base name + hash suffix)
	ConfigMapName string
	// ConfigMap is the applied ConfigMap object
	ConfigMap *corev1.ConfigMap
}

// Apply creates or updates a ConfigMap with a content-based hash suffix using server-side apply.
// It also cleans up old ConfigMaps that are no longer in use.
//
// The function:
// 1. Generates a hash suffix from the content
// 2. Creates or updates a ConfigMap with the hashed name using server-side apply
// 3. Sets the owner reference for garbage collection
// 4. Cleans up old ConfigMaps with the same base name but different hash suffixes
func (h *HashedConfigMap) Apply(ctx context.Context, content string, owner client.Object) (*Result, error) {
	log := logf.FromContext(ctx)

	// Generate hash suffix for the ConfigMap name
	hashSuffix := GenerateHashSuffix(content)
	configMapName := fmt.Sprintf("%s-%s", h.baseName, hashSuffix)

	log.Info("Applying hashed ConfigMap", "name", configMapName, "namespace", h.namespace)

	// Create the ConfigMap
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: h.namespace,
			Labels: map[string]string{
				h.label: "true",
			},
		},
		Data: map[string]string{
			h.dataKey: content,
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(owner, configMap, h.scheme); err != nil {
		return nil, fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Apply using server-side apply
	patchOpts := []client.PatchOption{client.FieldOwner(h.fieldManager), client.ForceOwnership}
	if err := h.client.Patch(ctx, configMap, client.Apply, patchOpts...); err != nil {
		return nil, fmt.Errorf("failed to apply ConfigMap %s: %w", configMapName, err)
	}

	// Clean up old ConfigMaps
	if err := h.cleanupOld(ctx, configMapName); err != nil {
		log.Error(err, "Failed to cleanup old ConfigMaps")
		// Don't return error - cleanup failure shouldn't block
	}

	return &Result{
		ConfigMapName: configMapName,
		ConfigMap:     configMap,
	}, nil
}

// cleanupOld removes old ConfigMaps that are no longer in use.
func (h *HashedConfigMap) cleanupOld(ctx context.Context, currentConfigMapName string) error {
	log := logf.FromContext(ctx)

	// List all ConfigMaps with the managed label
	configMapList := &corev1.ConfigMapList{}
	labelSelector := labels.SelectorFromSet(map[string]string{
		h.label: "true",
	})

	if err := h.client.List(ctx, configMapList,
		client.InNamespace(h.namespace),
		client.MatchingLabelsSelector{Selector: labelSelector},
	); err != nil {
		return fmt.Errorf("failed to list ConfigMaps: %w", err)
	}

	// Delete ConfigMaps that are not the current one
	prefix := h.baseName + "-"
	for _, cm := range configMapList.Items {
		if cm.Name != currentConfigMapName && strings.HasPrefix(cm.Name, prefix) {
			log.Info("Deleting old hashed ConfigMap", "name", cm.Name)
			if err := h.client.Delete(ctx, &cm); err != nil && !errors.IsNotFound(err) {
				log.Error(err, "Failed to delete old ConfigMap", "name", cm.Name)
			}
		}
	}

	return nil
}

// GenerateHashSuffix generates a short hash suffix from content, similar to kustomize.
// It uses the first HashSuffixLength characters of a SHA256 hash.
func GenerateHashSuffix(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])[:HashSuffixLength]
}

// BuildConfigMapName returns the full ConfigMap name for given base name and content.
// This is useful when you need to know the ConfigMap name without applying.
func BuildConfigMapName(baseName, content string) string {
	return fmt.Sprintf("%s-%s", baseName, GenerateHashSuffix(content))
}
