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

package tracking

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	testOwnerLabel = "test.example.com/owner"
	testOwnerValue = "test-owner"
	testNamespace  = "test-namespace"
)

var configMapGVK = schema.GroupVersionKind{
	Group:   "",
	Version: "v1",
	Kind:    "ConfigMap",
}

func TestNewClient(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	g.Expect(tc).NotTo(BeNil())
	g.Expect(tc.tracked).NotTo(BeNil())
	g.Expect(tc.tracked).To(BeEmpty())
}

func TestClient_ApplyObject(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	err := tc.ApplyObject(ctx, cm, "test-manager")
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "test-cm")).To(BeTrue())

	// Verify the resource exists in the cluster
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "test-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Data["key"]).To(Equal("value"))
}

func TestClient_Patch(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
		Data: map[string]string{
			"key": "old-value",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	tc := NewClient(fakeClient, scheme)

	// Patch the ConfigMap using server-side apply
	patched := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
		Data: map[string]string{
			"key": "new-value",
		},
	}

	err := tc.Patch(ctx, patched, client.Apply, client.FieldOwner("test-manager"), client.ForceOwnership)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "existing-cm")).To(BeTrue())
}

func TestClient_Create(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "created-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	err := tc.Create(ctx, cm)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "created-cm")).To(BeTrue())
}

func TestClient_Create_AlreadyExists(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Pre-create the object in the cluster
	existing := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	tc := NewClient(fakeClient, scheme)

	// Try to create the same object
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	err := tc.Create(ctx, cm)

	// Verify AlreadyExists error is returned
	g.Expect(errors.IsAlreadyExists(err)).To(BeTrue(), "expected AlreadyExists error")

	// Verify the resource is still tracked (to prevent orphan cleanup from deleting it)
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "existing-cm")).To(BeTrue())
}

func TestClient_Update(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	existing := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "update-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
		Data: map[string]string{
			"key": "old-value",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	tc := NewClient(fakeClient, scheme)

	// First fetch the object to get its resourceVersion
	var fetched corev1.ConfigMap
	err := fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "update-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())

	// Update it
	fetched.Data["key"] = "new-value"
	err = tc.Update(ctx, &fetched)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "update-cm")).To(BeTrue())
}

func TestClient_TrackedResources(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	// Apply multiple resources
	for i, name := range []string{"cm-1", "cm-2", "cm-3"} {
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespace,
			},
			Data: map[string]string{
				"index": string(rune('0' + i)),
			},
		}
		err := tc.ApplyObject(ctx, cm, "test-manager")
		g.Expect(err).NotTo(HaveOccurred())
	}

	// Verify all resources are tracked
	tracked := tc.TrackedResources()
	g.Expect(tracked).To(HaveLen(3))

	// Verify each resource is tracked
	for _, name := range []string{"cm-1", "cm-2", "cm-3"} {
		g.Expect(tc.IsTracked(configMapGVK, testNamespace, name)).To(BeTrue())
	}
}

func TestClient_CleanupOrphans(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Create pre-existing resources - some will be "orphans"
	existing1 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keep-this",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	existing2 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-this",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	existing3 := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "different-owner",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: "different-owner", // Different owner - should not be deleted
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existing1, existing2, existing3).
		Build()
	tc := NewClient(fakeClient, scheme)

	// Apply only one of the owned resources - the other becomes an orphan
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keep-this",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	err := tc.ApplyObject(ctx, cm, "test-manager")
	g.Expect(err).NotTo(HaveOccurred())

	// Run cleanup
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "keep-this" still exists
	var kept corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "keep-this"}, &kept)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "delete-this" was deleted
	var deleted corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "delete-this"}, &deleted)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound error")

	// Verify "different-owner" still exists (different owner label value)
	var differentOwner corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "different-owner"}, &differentOwner)
	g.Expect(err).NotTo(HaveOccurred())
}

func TestClient_CleanupOrphans_ClusterScoped(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Create cluster-scoped resources (Namespaces)
	ns1 := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "keep-ns",
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	ns2 := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "delete-ns",
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2).
		Build()
	tc := NewClient(fakeClient, scheme)

	// Apply only one namespace
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "keep-ns",
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	err := tc.ApplyObject(ctx, ns, "test-manager")
	g.Expect(err).NotTo(HaveOccurred())

	// Run cleanup for Namespace GVK
	namespaceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{namespaceGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "keep-ns" still exists
	var keptNS corev1.Namespace
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "keep-ns"}, &keptNS)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "delete-ns" was deleted
	var deletedNS corev1.Namespace
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "delete-ns"}, &deletedNS)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound error")
}

func TestClient_CleanupOrphans_MultipleGVKs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Create ConfigMaps and Secrets
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "orphan-secret",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm, secret).
		Build()
	tc := NewClient(fakeClient, scheme)

	// Don't apply anything - both resources are orphans

	// Run cleanup for both GVKs
	secretGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
	err := tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK, secretGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify both were deleted
	var deletedCM corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "orphan-cm"}, &deletedCM)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound error for ConfigMap")

	var deletedSecret corev1.Secret
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "orphan-secret"}, &deletedSecret)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound error for Secret")
}

func TestClient_CreateOrUpdate_Create(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-cm",
			Namespace: testNamespace,
		},
	}

	result, err := tc.CreateOrUpdate(ctx, cm, func() error {
		cm.Data = map[string]string{"key": "value"}
		return nil
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(controllerutil.OperationResultCreated))

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "new-cm")).To(BeTrue())

	// Verify the resource exists in the cluster with correct data
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "new-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Data["key"]).To(Equal("value"))
}

func TestClient_CreateOrUpdate_Update(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"key": "old-value",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
		},
	}

	result, err := tc.CreateOrUpdate(ctx, cm, func() error {
		cm.Data = map[string]string{"key": "new-value"}
		return nil
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(controllerutil.OperationResultUpdated))

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "existing-cm")).To(BeTrue())

	// Verify the resource was updated
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "existing-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Data["key"]).To(Equal("new-value"))
}

func TestClient_CreateOrUpdate_Unchanged(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	existing := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unchanged-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "unchanged-cm",
			Namespace: testNamespace,
		},
	}

	result, err := tc.CreateOrUpdate(ctx, cm, func() error {
		// Don't change anything - the mutate function doesn't modify the object
		return nil
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(controllerutil.OperationResultNone))

	// Verify the resource is STILL tracked even though no update occurred
	// This is the key behavior - objects must be tracked to prevent orphan cleanup
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "unchanged-cm")).To(BeTrue())
}

func TestClient_CreateOrUpdate_Error(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient, scheme)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "error-cm",
			Namespace: testNamespace,
		},
	}

	result, err := tc.CreateOrUpdate(ctx, cm, func() error {
		return errors.NewBadRequest("mutate error")
	})

	g.Expect(err).To(HaveOccurred())
	g.Expect(result).To(Equal(controllerutil.OperationResultNone))

	// Verify the resource is NOT tracked when there's an error
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "error-cm")).To(BeFalse())
}

func TestResourceKey_String(t *testing.T) {
	tests := []struct {
		name     string
		key      ResourceKey
		expected string
	}{
		{
			name: "namespaced resource",
			key: ResourceKey{
				GVK:       configMapGVK,
				Namespace: "my-namespace",
				Name:      "my-configmap",
			},
			expected: "ConfigMap/my-namespace/my-configmap",
		},
		{
			name: "cluster-scoped resource",
			key: ResourceKey{
				GVK:       schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"},
				Namespace: "",
				Name:      "my-namespace",
			},
			expected: "Namespace/my-namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(tt.key.String()).To(Equal(tt.expected))
		})
	}
}

func setupScheme(g *WithT) *runtime.Scheme {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	g.Expect(err).NotTo(HaveOccurred())
	return scheme
}
