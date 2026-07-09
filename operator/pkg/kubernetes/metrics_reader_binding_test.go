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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestHasMetricsScraperBindingAnnotation(t *testing.T) {
	t.Parallel()

	if HasMetricsScraperBindingAnnotation(nil) {
		t.Fatal("nil object should not have annotation")
	}
	if HasMetricsScraperBindingAnnotation(&rbacv1.ClusterRoleBinding{}) {
		t.Fatal("missing annotation should return false")
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{MetricsScraperBindingAnnotation: "true"},
		},
	}
	if !HasMetricsScraperBindingAnnotation(crb) {
		t.Fatal("expected annotation to be detected")
	}
}

func TestApplyMetricsScraperBindingSubjects(t *testing.T) {
	t.Parallel()

	subjects := []rbacv1.Subject{{
		Kind: rbacv1.ServiceAccountKind, Name: "prometheus", Namespace: "monitoring",
	}}

	crb := &rbacv1.ClusterRoleBinding{}
	if err := ApplyMetricsScraperBindingSubjects(crb, subjects); err != nil {
		t.Fatalf("ClusterRoleBinding: %v", err)
	}
	if len(crb.Subjects) != 1 || crb.Subjects[0].Name != "prometheus" {
		t.Fatalf("subjects not applied: %#v", crb.Subjects)
	}

	u := &unstructured.Unstructured{}
	u.SetAPIVersion("rbac.authorization.k8s.io/v1")
	u.SetKind("ClusterRoleBinding")
	if err := ApplyMetricsScraperBindingSubjects(u, subjects); err != nil {
		t.Fatalf("Unstructured: %v", err)
	}
	got, found, err := unstructured.NestedSlice(u.Object, "subjects")
	if err != nil || !found || len(got) != 1 {
		t.Fatalf("unstructured subjects: found=%v err=%v got=%#v", found, err, got)
	}

	if err := ApplyMetricsScraperBindingSubjects(&corev1.ConfigMap{}, subjects); err == nil {
		t.Fatal("expected unsupported type error")
	}
}

func TestEnsureMetricsReaderBindingValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
	subjects := []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: "prometheus", Namespace: "monitoring"}}

	err := EnsureMetricsReaderBinding(ctx, c, "", "metrics-reader", subjects)
	if err == nil {
		t.Fatal("expected error for empty binding name")
	}
	err = EnsureMetricsReaderBinding(ctx, c, "binding", "", subjects)
	if err == nil {
		t.Fatal("expected error for empty cluster role name")
	}
	err = EnsureMetricsReaderBinding(ctx, c, "binding", "metrics-reader", nil)
	if err == nil {
		t.Fatal("expected error for empty subjects")
	}
}

func TestEnsureMetricsReaderBindingCreateAndPatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = rbacv1.AddToScheme(scheme)

	subjects := []rbacv1.Subject{{
		Kind: rbacv1.ServiceAccountKind, Name: "prometheus", Namespace: "monitoring",
	}}
	updated := []rbacv1.Subject{{
		Kind:      rbacv1.ServiceAccountKind,
		Name:      MetricsScraperServiceAccountName,
		Namespace: "build-service",
	}}

	t.Run("create", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		if err := EnsureMetricsReaderBinding(ctx, c, "prometheus-test", "test-metrics-reader", subjects); err != nil {
			t.Fatalf("create: %v", err)
		}
		got := &rbacv1.ClusterRoleBinding{}
		if err := c.Get(ctx, client.ObjectKey{Name: "prometheus-test"}, got); err != nil {
			t.Fatalf("get created binding: %v", err)
		}
		if got.RoleRef.Name != "test-metrics-reader" || len(got.Subjects) != 1 {
			t.Fatalf("unexpected binding: %#v", got)
		}
		if got.Annotations[MetricsScraperBindingAnnotation] != "true" {
			t.Fatalf("missing scraper binding annotation: %#v", got.Annotations)
		}
	})

	t.Run("patch", func(t *testing.T) {
		t.Parallel()
		existing := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "prometheus-test-patch"},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     "test-metrics-reader",
			},
			Subjects: subjects,
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		if err := EnsureMetricsReaderBinding(ctx, c, "prometheus-test-patch", "test-metrics-reader", updated); err != nil {
			t.Fatalf("patch: %v", err)
		}
		got := &rbacv1.ClusterRoleBinding{}
		if err := c.Get(ctx, client.ObjectKey{Name: "prometheus-test-patch"}, got); err != nil {
			t.Fatalf("get patched binding: %v", err)
		}
		if len(got.Subjects) != 1 || got.Subjects[0].Name != MetricsScraperServiceAccountName {
			t.Fatalf("subjects not patched: %#v", got.Subjects)
		}
		if got.Annotations[MetricsScraperBindingAnnotation] != "true" {
			t.Fatalf("annotation not set on patch: %#v", got.Annotations)
		}
	})
}

func TestEnsureMetricsReaderBindingGetError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = rbacv1.AddToScheme(scheme)
	getErr := errors.New("get failed")
	c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return getErr
		},
	}).Build()

	err := EnsureMetricsReaderBinding(ctx, c, "prometheus-test", "test-metrics-reader", []rbacv1.Subject{
		{Kind: rbacv1.ServiceAccountKind, Name: "prometheus", Namespace: "monitoring"},
	})
	if err == nil {
		t.Fatal("expected get error")
	}
	if err.Error() == "" {
		t.Fatalf("expected wrapped get error, got %v", err)
	}
}
