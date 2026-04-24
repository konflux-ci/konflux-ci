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

// Package common provides shared utilities used across multiple controllers.
package common

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

// EnsureNamespaceExists finds the Namespace object in the embedded manifests
// for the given component, validates that its name matches expectedName, and
// applies it using the tracking client. This is a shared utility that replaces
// per-controller copies of the same logic.
func EnsureNamespaceExists(ctx context.Context, store *manifests.ObjectStore, component manifests.Component, expectedName string, tc *tracking.Client) error {
	objects, err := store.GetForComponent(component)
	if err != nil {
		return fmt.Errorf("failed to get parsed manifests for %s: %w", component, err)
	}

	for _, obj := range objects {
		if namespace, ok := obj.(*corev1.Namespace); ok {
			if namespace.Name != expectedName {
				return fmt.Errorf(
					"unexpected namespace name in manifest: expected %s, got %s", expectedName, namespace.Name)
			}
			if err := tc.ApplyOwned(ctx, namespace); err != nil {
				return fmt.Errorf("failed to apply namespace %s: %w", namespace.Name, err)
			}
		}
	}
	return nil
}
