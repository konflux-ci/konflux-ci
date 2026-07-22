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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TEMP EXPERIMENT: ResyncOperandServiceMonitor is a no-op on experiment/uwm-no-sm-resync.
// These tests lock that behavior; restore patch assertions when reverting the experiment.

func TestResyncOperandServiceMonitor_NoOpDoesNotPatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetNamespace("build-service")
	sm.SetName("build-service")
	c := fake.NewClientBuilder().WithObjects(sm).Build()

	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force:                 true,
		Reason:                ServiceMonitorResyncReasonCASync,
		SecretResourceVersion: "tok-1",
		CAResourceVersion:     "ca-9",
		Clock:                 testclock.NewFakeClock(now),
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}
	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(updated.GetAnnotations()) != 0 {
		t.Fatalf("expected no annotations on experiment no-op, got %#v", updated.GetAnnotations())
	}
}

func TestResyncOperandServiceMonitor_NoOpPreservesExistingAnnotations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetNamespace("build-service")
	sm.SetName("build-service")
	sm.SetAnnotations(map[string]string{
		ServiceMonitorResyncAnnotation: "2026-07-12T07:00:00Z",
	})

	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(sm).Build()
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force:  true,
		Reason: ServiceMonitorResyncReasonTokenMinted,
		Clock:  testclock.NewFakeClock(now),
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get updated SM: %v", err)
	}
	if updated.GetAnnotations()[ServiceMonitorResyncAnnotation] != "2026-07-12T07:00:00Z" {
		t.Fatalf("expected existing annotation unchanged, got %#v", updated.GetAnnotations())
	}
}

func TestResyncOperandServiceMonitor_Validation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()

	err := ResyncOperandServiceMonitor(ctx, c, "", "build-service", ServiceMonitorResyncOptions{Force: true})
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
	err = ResyncOperandServiceMonitor(ctx, c, "build-service", "", ServiceMonitorResyncOptions{Force: true})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestResyncOperandServiceMonitor_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force: true,
	})
	if err != nil {
		t.Fatalf("expected no error when ServiceMonitor is absent: %v", err)
	}
}

func TestServiceMonitorResyncHelpers(t *testing.T) {
	t.Parallel()

	if ServiceMonitorResyncSettlePending(nil) {
		t.Fatal("nil SM should not be settle pending")
	}
	if ServiceMonitorResyncSecretRV(nil) != "" {
		t.Fatal("nil SM should have empty secret RV")
	}
	if ServiceMonitorResyncCARV(nil) != "" {
		t.Fatal("nil SM should have empty CA RV")
	}

	sm := &unstructured.Unstructured{}
	if ServiceMonitorResyncSettlePending(sm) {
		t.Fatal("empty annotations should not be settle pending")
	}
	if ServiceMonitorResyncSecretRV(sm) != "" {
		t.Fatal("empty annotations should have empty secret RV")
	}
	if ServiceMonitorResyncCARV(sm) != "" {
		t.Fatal("empty annotations should have empty CA RV")
	}

	sm.SetAnnotations(map[string]string{
		ServiceMonitorResyncSettleAnnotation:   serviceMonitorResyncSettlePending,
		ServiceMonitorResyncSecretRVAnnotation: "42",
		ServiceMonitorResyncCARVAnnotation:     "ca-7",
	})
	if !ServiceMonitorResyncSettlePending(sm) {
		t.Fatal("expected settle pending")
	}
	if ServiceMonitorResyncSecretRV(sm) != "42" {
		t.Fatalf("secret RV: got %q", ServiceMonitorResyncSecretRV(sm))
	}
	if ServiceMonitorResyncCARV(sm) != "ca-7" {
		t.Fatalf("CA RV: got %q", ServiceMonitorResyncCARV(sm))
	}
}
