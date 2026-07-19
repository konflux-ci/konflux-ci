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

func TestResyncOperandServiceMonitor_SetsCARV(t *testing.T) {
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
	if updated.GetAnnotations()[ServiceMonitorResyncCARVAnnotation] != "ca-9" {
		t.Fatalf("ca rv: %#v", updated.GetAnnotations())
	}
	if ServiceMonitorResyncCARV(updated) != "ca-9" {
		t.Fatalf("helper CARV: %q", ServiceMonitorResyncCARV(updated))
	}
}

func TestResyncOperandServiceMonitorSetsAnnotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	sm := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"endpoints": []any{map[string]any{"port": "https"}},
		},
	}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetNamespace("build-service")
	sm.SetName("build-service")

	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(sm).Build()
	clk := testclock.NewFakeClock(now)
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force:                 true,
		Reason:                ServiceMonitorResyncReasonTokenMinted,
		SecretResourceVersion: "100",
		MarkSettlePending:     true,
		Clock:                 clk,
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get updated SM: %v", err)
	}
	annotations := updated.GetAnnotations()
	if got := annotations[ServiceMonitorResyncAnnotation]; got != now.UTC().Format(time.RFC3339) {
		t.Fatalf("resync annotation: got %q want %q", got, now.UTC().Format(time.RFC3339))
	}
	if annotations[ServiceMonitorResyncReasonAnnotation] != ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("resync reason: got %q", annotations[ServiceMonitorResyncReasonAnnotation])
	}
	if annotations[ServiceMonitorResyncSecretRVAnnotation] != "100" {
		t.Fatalf("secret rv: got %q", annotations[ServiceMonitorResyncSecretRVAnnotation])
	}
	if annotations[ServiceMonitorResyncSettleAnnotation] != serviceMonitorResyncSettlePending {
		t.Fatalf("settle pending: got %q", annotations[ServiceMonitorResyncSettleAnnotation])
	}
}

func TestResyncOperandServiceMonitorSkipsWhenAnnotated(t *testing.T) {
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
	clk := testclock.NewFakeClock(now)
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Clock: clk,
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get updated SM: %v", err)
	}
	if updated.GetAnnotations()[ServiceMonitorResyncAnnotation] != "2026-07-12T07:00:00Z" {
		t.Fatalf("expected resync annotation to remain unchanged")
	}
}

func TestResyncOperandServiceMonitorForceWhenAnnotated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetNamespace("build-service")
	sm.SetName("build-service")
	sm.SetAnnotations(map[string]string{
		ServiceMonitorResyncAnnotation:       "2026-07-12T07:00:00Z",
		ServiceMonitorResyncSettleAnnotation: serviceMonitorResyncSettlePending,
	})

	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(sm).Build()
	clk := testclock.NewFakeClock(now)
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force:              true,
		Reason:             ServiceMonitorResyncReasonSettleRetry,
		ClearSettlePending: true,
		Clock:              clk,
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get updated SM: %v", err)
	}
	annotations := updated.GetAnnotations()
	if annotations[ServiceMonitorResyncReasonAnnotation] != ServiceMonitorResyncReasonSettleRetry {
		t.Fatalf("reason: got %q", annotations[ServiceMonitorResyncReasonAnnotation])
	}
	if _, ok := annotations[ServiceMonitorResyncSettleAnnotation]; ok {
		t.Fatalf("expected settle annotation to be cleared")
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

func TestResyncOperandServiceMonitor_DefaultClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	sm.SetNamespace("build-service")
	sm.SetName("build-service")

	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(sm).Build()
	err := ResyncOperandServiceMonitor(ctx, c, "build-service", "build-service", ServiceMonitorResyncOptions{
		Force:  true,
		Reason: ServiceMonitorResyncReasonSecretSync,
	})
	if err != nil {
		t.Fatalf("resync with default clock: %v", err)
	}
}

func TestResyncOperandServiceMonitor_MarkSettleWhenAnnotated(t *testing.T) {
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
		MarkSettlePending: true,
		Clock:             testclock.NewFakeClock(now),
	})
	if err != nil {
		t.Fatalf("resync: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: "build-service", Name: "build-service"}, updated); err != nil {
		t.Fatalf("get updated SM: %v", err)
	}
	if updated.GetAnnotations()[ServiceMonitorResyncSettleAnnotation] != serviceMonitorResyncSettlePending {
		t.Fatalf("expected settle pending to be set")
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
