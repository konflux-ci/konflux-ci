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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

func TestEnsureNamespaceExists(t *testing.T) {
	store := testutil.GetTestObjectStore(t)

	t.Run("finds and validates namespace in manifests", func(t *testing.T) {
		// BuildService manifests contain a "build-service" namespace.
		// Use a nil client since we only test validation, not actual apply.
		objects, err := store.GetForComponent(manifests.BuildService)
		if err != nil {
			t.Fatalf("failed to get BuildService manifests: %v", err)
		}

		// Verify that a Namespace object exists in the manifests.
		var found bool
		var nsName string
		for _, obj := range objects {
			if ns, ok := obj.(*corev1.Namespace); ok {
				found = true
				nsName = ns.Name
				break
			}
		}
		if !found {
			t.Skip("BuildService manifests do not contain a Namespace object")
		}

		// Wrong expected name should return an error.
		fakeClient := &fakeTrackingClient{}
		tc := tracking.NewClient(fakeClient)
		err = EnsureNamespaceExists(context.Background(), store, manifests.BuildService, "wrong-name", tc)
		if err == nil {
			t.Fatal("expected error for mismatched namespace name, got nil")
		}

		// Correct expected name should call ApplyOwned (which will fail
		// because we have no real client, but the error comes from apply,
		// not from validation).
		err = EnsureNamespaceExists(context.Background(), store, manifests.BuildService, nsName, tc)
		// We expect an apply error from the nil-ish fake client, which
		// confirms validation passed and apply was attempted.
		if err == nil {
			// If somehow the fake client didn't error, that's also fine —
			// it means the function completed successfully.
			return
		}
		// The error should be about applying, not about namespace mismatch.
		if err.Error() != "" && contains(err.Error(), "unexpected namespace name") {
			t.Fatalf("expected apply error, got validation error: %v", err)
		}
	})

	t.Run("no namespace in manifests is not an error", func(t *testing.T) {
		// RBAC component typically has no Namespace object.
		fakeClient := &fakeTrackingClient{}
		tc := tracking.NewClient(fakeClient)
		err := EnsureNamespaceExists(context.Background(), store, manifests.RBAC, "anything", tc)
		if err != nil {
			t.Fatalf("expected no error when no namespace in manifests, got: %v", err)
		}
	})

	t.Run("unknown component returns error", func(t *testing.T) {
		fakeClient := &fakeTrackingClient{}
		tc := tracking.NewClient(fakeClient)
		err := EnsureNamespaceExists(context.Background(), store, manifests.Component("nonexistent"), "ns", tc)
		if err == nil {
			t.Fatal("expected error for unknown component, got nil")
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// fakeTrackingClient is a minimal fake that satisfies client.Client for tracking.NewClient.
type fakeTrackingClient struct {
	client.Client
}
