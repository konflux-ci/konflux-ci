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
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

const (
	testOwnerLabel     = "test.example.com/owner"
	testComponentLabel = "test.example.com/component"
	testOwnerValue     = "test-owner"
	testComponent      = "test-component"
	testFieldManager   = "test-manager"
	testNamespace      = "test-namespace"
)

var configMapGVK = schema.GroupVersionKind{
	Group:   "",
	Version: "v1",
	Kind:    "ConfigMap",
}

// createTestOwner creates a ConfigMap to use as an owner for testing.
// ConfigMaps work as owners because they have the required metadata.
func createTestOwner(g *WithT, fakeClient client.Client) *corev1.ConfigMap {
	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testOwnerValue,
			Namespace: testNamespace,
			UID:       "test-owner-uid",
		},
	}
	err := fakeClient.Create(context.Background(), owner)
	g.Expect(err).NotTo(HaveOccurred())
	return owner
}

func TestNewClient(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient)

	g.Expect(tc).NotTo(BeNil())
	g.Expect(tc.tracked).NotTo(BeNil())
	g.Expect(tc.tracked).To(BeEmpty())
}

func TestClient_ApplyObject(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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
	tc := NewClient(fakeClient)

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

func TestNewClientWithOwnership(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	g.Expect(tc).NotTo(BeNil())
	g.Expect(tc.tracked).NotTo(BeNil())
	g.Expect(tc.tracked).To(BeEmpty())
	g.Expect(tc.ownership).NotTo(BeNil())
	g.Expect(tc.ownership.Owner).To(Equal(owner))
	g.Expect(tc.ownership.OwnerLabelKey).To(Equal(testOwnerLabel))
	g.Expect(tc.ownership.ComponentLabelKey).To(Equal(testComponentLabel))
	g.Expect(tc.ownership.Component).To(Equal(testComponent))
	g.Expect(tc.ownership.FieldManager).To(Equal(testFieldManager))
}

func TestClient_SetOwnership(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-cm",
			Namespace: testNamespace,
		},
	}

	err := tc.SetOwnership(cm)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify labels were set
	g.Expect(cm.Labels).To(HaveKeyWithValue(testOwnerLabel, testOwnerValue))
	g.Expect(cm.Labels).To(HaveKeyWithValue(testComponentLabel, testComponent))

	// Verify owner reference was set
	g.Expect(cm.OwnerReferences).To(HaveLen(1))
	g.Expect(cm.OwnerReferences[0].Name).To(Equal(testOwnerValue))
	g.Expect(cm.OwnerReferences[0].UID).To(Equal(owner.UID))
	g.Expect(*cm.OwnerReferences[0].Controller).To(BeTrue())
}

func TestClient_SetOwnership_PreservesExistingLabels(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				"existing-label": "existing-value",
			},
		},
	}

	err := tc.SetOwnership(cm)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify existing labels are preserved
	g.Expect(cm.Labels).To(HaveKeyWithValue("existing-label", "existing-value"))
	// Verify ownership labels were added
	g.Expect(cm.Labels).To(HaveKeyWithValue(testOwnerLabel, testOwnerValue))
	g.Expect(cm.Labels).To(HaveKeyWithValue(testComponentLabel, testComponent))
}

func TestClient_SetOwnership_ErrorWithoutOwnershipConfig(t *testing.T) {
	g := NewWithT(t)

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create client WITHOUT ownership config
	tc := NewClient(fakeClient)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "owned-cm",
			Namespace: testNamespace,
		},
	}

	err := tc.SetOwnership(cm)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("ownership config"))
}

func TestClient_ApplyOwned(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "applied-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	err := tc.ApplyOwned(ctx, cm)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "applied-cm")).To(BeTrue())

	// Verify the resource exists in the cluster
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "applied-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify labels were set
	g.Expect(fetched.Labels).To(HaveKeyWithValue(testOwnerLabel, testOwnerValue))
	g.Expect(fetched.Labels).To(HaveKeyWithValue(testComponentLabel, testComponent))

	// Verify owner reference was set
	g.Expect(fetched.OwnerReferences).To(HaveLen(1))
	g.Expect(fetched.OwnerReferences[0].Name).To(Equal(testOwnerValue))

	// Verify data was applied
	g.Expect(fetched.Data["key"]).To(Equal("value"))
}

func TestClient_ApplyOwned_ErrorWithoutOwnershipConfig(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create client WITHOUT ownership config
	tc := NewClient(fakeClient)

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "applied-cm",
			Namespace: testNamespace,
		},
	}

	err := tc.ApplyOwned(ctx, cm)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("ownership config"))

	// Verify the resource is NOT tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "applied-cm")).To(BeFalse())
}

func TestClient_ApplyOwned_UpdatesExistingResource(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Pre-create a resource without ownership
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
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-cm",
			Namespace: testNamespace,
		},
		Data: map[string]string{
			"key": "new-value",
		},
	}

	err := tc.ApplyOwned(ctx, cm)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the resource was updated with ownership
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "existing-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify labels were added
	g.Expect(fetched.Labels).To(HaveKeyWithValue(testOwnerLabel, testOwnerValue))
	g.Expect(fetched.Labels).To(HaveKeyWithValue(testComponentLabel, testComponent))

	// Verify data was updated
	g.Expect(fetched.Data["key"]).To(Equal("new-value"))
}

func TestClient_CreateOrUpdate_WithSetOwnership(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "createorupdate-cm",
			Namespace: testNamespace,
		},
	}

	// Use CreateOrUpdate with SetOwnership in the mutate function
	result, err := tc.CreateOrUpdate(ctx, cm, func() error {
		if err := tc.SetOwnership(cm); err != nil {
			return err
		}
		cm.Data = map[string]string{"key": "value"}
		return nil
	})

	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(result).To(Equal(controllerutil.OperationResultCreated))

	// Verify the resource is tracked
	g.Expect(tc.IsTracked(configMapGVK, testNamespace, "createorupdate-cm")).To(BeTrue())

	// Verify the resource has ownership set
	var fetched corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "createorupdate-cm"}, &fetched)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(fetched.Labels).To(HaveKeyWithValue(testOwnerLabel, testOwnerValue))
	g.Expect(fetched.OwnerReferences).To(HaveLen(1))
}

// TestIsNoKindMatchError tests the IsNoKindMatchError helper function.
func TestIsNoKindMatchError(t *testing.T) {
	t.Run("returns true for NoKindMatchError", func(t *testing.T) {
		g := NewWithT(t)
		noKindErr := &meta.NoKindMatchError{
			GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "Certificate"},
		}
		g.Expect(IsNoKindMatchError(noKindErr)).To(BeTrue())
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		g := NewWithT(t)
		otherErr := fmt.Errorf("some other error")
		g.Expect(IsNoKindMatchError(otherErr)).To(BeFalse())
	})

	t.Run("returns false for nil error", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(IsNoKindMatchError(nil)).To(BeFalse())
	})

	t.Run("returns false for wrapped non-NoKindMatchError", func(t *testing.T) {
		g := NewWithT(t)
		wrappedErr := fmt.Errorf("wrapped: %w", fmt.Errorf("inner error"))
		g.Expect(IsNoKindMatchError(wrappedErr)).To(BeFalse())
	})

	t.Run("returns true for wrapped NoKindMatchError", func(t *testing.T) {
		g := NewWithT(t)
		noKindErr := &meta.NoKindMatchError{
			GroupKind: schema.GroupKind{Group: "trust.cert-manager.io", Kind: "Bundle"},
		}
		wrappedErr := fmt.Errorf("failed to list resources: %w", noKindErr)
		g.Expect(IsNoKindMatchError(wrappedErr)).To(BeTrue())
	})

	t.Run("returns true for deeply wrapped NoKindMatchError", func(t *testing.T) {
		g := NewWithT(t)
		noKindErr := &meta.NoKindMatchError{
			GroupKind: schema.GroupKind{Group: "kyverno.io", Kind: "Policy"},
		}
		wrappedOnce := fmt.Errorf("cleanup failed: %w", noKindErr)
		wrappedTwice := fmt.Errorf("reconcile error: %w", wrappedOnce)
		g.Expect(IsNoKindMatchError(wrappedTwice)).To(BeTrue())
	})

	t.Run("returns false for standard API errors", func(t *testing.T) {
		g := NewWithT(t)
		notFoundErr := errors.NewNotFound(schema.GroupResource{Group: "", Resource: "configmaps"}, "test")
		g.Expect(IsNoKindMatchError(notFoundErr)).To(BeFalse())

		forbiddenErr := errors.NewForbidden(
			schema.GroupResource{Group: "", Resource: "secrets"},
			"test",
			fmt.Errorf("access denied"),
		)
		g.Expect(IsNoKindMatchError(forbiddenErr)).To(BeFalse())
	})
}

func TestClusterScopedAllowList_IsAllowed(t *testing.T) {
	namespaceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
	clusterRoleGVK := schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"}

	tests := []struct {
		name      string
		allowList ClusterScopedAllowList
		gvk       schema.GroupVersionKind
		namespace string
		resName   string
		expected  bool
	}{
		{
			name:      "namespaced resource is always allowed",
			allowList: ClusterScopedAllowList{namespaceGVK: sets.New("allowed-ns")},
			gvk:       configMapGVK,
			namespace: "some-namespace",
			resName:   "any-configmap",
			expected:  true,
		},
		{
			name:      "nil allow list allows all",
			allowList: nil,
			gvk:       namespaceGVK,
			namespace: "",
			resName:   "any-namespace",
			expected:  true,
		},
		{
			name:      "empty allow list allows all",
			allowList: ClusterScopedAllowList{},
			gvk:       namespaceGVK,
			namespace: "",
			resName:   "any-namespace",
			expected:  true,
		},
		{
			name:      "GVK not in allow list is allowed",
			allowList: ClusterScopedAllowList{namespaceGVK: sets.New("allowed-ns")},
			gvk:       clusterRoleGVK,
			namespace: "",
			resName:   "any-clusterrole",
			expected:  true,
		},
		{
			name:      "cluster-scoped resource in allow list is allowed",
			allowList: ClusterScopedAllowList{namespaceGVK: sets.New("allowed-ns", "another-ns")},
			gvk:       namespaceGVK,
			namespace: "",
			resName:   "allowed-ns",
			expected:  true,
		},
		{
			name:      "cluster-scoped resource NOT in allow list is denied",
			allowList: ClusterScopedAllowList{namespaceGVK: sets.New("allowed-ns")},
			gvk:       namespaceGVK,
			namespace: "",
			resName:   "not-allowed-ns",
			expected:  false,
		},
		{
			name:      "empty names list in allow list denies all for that GVK",
			allowList: ClusterScopedAllowList{namespaceGVK: sets.New[string]()},
			gvk:       namespaceGVK,
			namespace: "",
			resName:   "any-namespace",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			result := tt.allowList.IsAllowed(tt.gvk, tt.namespace, tt.resName)
			g.Expect(result).To(Equal(tt.expected))
		})
	}
}

func TestClient_CleanupOrphans_WithClusterScopedAllowList(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	namespaceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}

	// Create cluster-scoped resources (Namespaces) with owner label
	allowedNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "allowed-ns",
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	blockedNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "blocked-ns", // This is NOT in the allow list
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	trackedNS := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tracked-ns", // This is in allow list and will be tracked
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(allowedNS, blockedNS, trackedNS).
		Build()
	tc := NewClient(fakeClient)

	// Track one of the namespaces (simulating it was applied during reconcile)
	trackedNSCopy := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tracked-ns",
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}
	err := tc.ApplyObject(ctx, trackedNSCopy, "test-manager")
	g.Expect(err).NotTo(HaveOccurred())

	// Define allow list - only "allowed-ns" and "tracked-ns" can be deleted
	allowList := ClusterScopedAllowList{
		namespaceGVK: sets.New("allowed-ns", "tracked-ns"),
	}

	// Run cleanup with allow list
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{namespaceGVK},
		WithClusterScopedAllowList(allowList))
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "tracked-ns" still exists (it was tracked during reconcile)
	var keptNS corev1.Namespace
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "tracked-ns"}, &keptNS)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify "allowed-ns" was deleted (not tracked, but in allow list)
	var deletedNS corev1.Namespace
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "allowed-ns"}, &deletedNS)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "expected NotFound error for allowed-ns")

	// Verify "blocked-ns" still exists (not tracked, but NOT in allow list - blocked from deletion)
	var blockedNSResult corev1.Namespace
	err = fakeClient.Get(ctx, client.ObjectKey{Name: "blocked-ns"}, &blockedNSResult)
	g.Expect(err).NotTo(HaveOccurred(), "blocked-ns should NOT be deleted because it's not in the allow list")
}

func TestClient_CleanupOrphans_AllowListDoesNotAffectNamespacedResources(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	namespaceGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}

	// Create a namespaced ConfigMap with owner label
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

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cm).
		Build()
	tc := NewClient(fakeClient)

	// Define allow list for Namespace only (should not affect ConfigMaps)
	allowList := ClusterScopedAllowList{
		namespaceGVK: sets.New("some-ns"), // Restrictive, but only for Namespace GVK
	}

	// Run cleanup for ConfigMap with the allow list
	err := tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK},
		WithClusterScopedAllowList(allowList))
	g.Expect(err).NotTo(HaveOccurred())

	// Verify ConfigMap was deleted (allow list doesn't restrict namespaced resources)
	var deletedCM corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "orphan-cm"}, &deletedCM)
	g.Expect(
		errors.IsNotFound(err)).To(BeTrue(),
		"expected NotFound error - namespaced resources should be deleted regardless of allow list",
	)
}

// TestClient_CleanupOrphans_SkipsResourcesWithoutOwnerReference verifies that resources
// with the owner label but without a matching owner reference are NOT deleted.
// This is a security measure to prevent deletion of resources that weren't created by the controller.
func TestClient_CleanupOrphans_SkipsResourcesWithoutOwnerReference(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	// Create tracking client WITH ownership config
	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	// Create a ConfigMap with the owner label but NO owner reference
	// This simulates an attacker adding the label to a resource they created
	labelOnlyCM := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "label-only-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
			// No OwnerReferences set!
		},
	}
	err := fakeClient.Create(ctx, labelOnlyCM)
	g.Expect(err).NotTo(HaveOccurred())

	// Run cleanup - this should NOT delete the resource because it lacks owner reference
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the ConfigMap still exists (not deleted because no owner reference)
	var cm corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "label-only-cm"}, &cm)
	g.Expect(err).NotTo(HaveOccurred(), "ConfigMap should NOT be deleted - it has no owner reference")
}

// TestClient_CleanupOrphans_SkipsResourcesWithWrongOwnerUID verifies that resources
// with the owner label but with an owner reference pointing to a different UID are NOT deleted.
// This prevents deletion if someone creates a fake owner reference with the right name but wrong UID.
func TestClient_CleanupOrphans_SkipsResourcesWithWrongOwnerUID(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	// Create tracking client WITH ownership config
	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	// Create a ConfigMap with owner label AND owner reference, but with WRONG UID
	wrongUIDCM := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wrong-uid-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       testOwnerValue, // Correct name
					UID:        "wrong-uid",    // Wrong UID!
					Controller: ptr.To(true),
				},
			},
		},
	}
	err := fakeClient.Create(ctx, wrongUIDCM)
	g.Expect(err).NotTo(HaveOccurred())

	// Run cleanup - this should NOT delete the resource because UID doesn't match
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the ConfigMap still exists (not deleted because UID doesn't match)
	var cm corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "wrong-uid-cm"}, &cm)
	g.Expect(err).NotTo(HaveOccurred(), "ConfigMap should NOT be deleted - owner UID doesn't match")
}

// TestClient_CleanupOrphans_DeletesResourcesWithCorrectOwnerReference verifies that resources
// with both the owner label AND a correct owner reference (name + UID) ARE deleted.
func TestClient_CleanupOrphans_DeletesResourcesWithCorrectOwnerReference(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	owner := createTestOwner(g, fakeClient)

	// Create tracking client WITH ownership config
	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	// Create a ConfigMap with owner label AND correct owner reference
	correctOwnerCM := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "correct-owner-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "ConfigMap",
					Name:       owner.GetName(),
					UID:        owner.GetUID(), // Correct UID!
					Controller: ptr.To(true),
				},
			},
		},
	}
	err := fakeClient.Create(ctx, correctOwnerCM)
	g.Expect(err).NotTo(HaveOccurred())

	// Run cleanup - this SHOULD delete the resource because owner reference matches
	err = tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the ConfigMap was deleted
	var cm corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "correct-owner-cm"}, &cm)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "ConfigMap should be deleted - it has correct owner reference")
}

// TestClient_CleanupOrphans_WithoutOwnershipConfig verifies that when no ownership config
// is set, the owner reference check is skipped (backward compatibility).
func TestClient_CleanupOrphans_WithoutOwnershipConfig(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)

	// Create a ConfigMap with owner label but no owner reference
	labelOnlyCM := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "label-only-cm",
			Namespace: testNamespace,
			Labels: map[string]string{
				testOwnerLabel: testOwnerValue,
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(labelOnlyCM).
		Build()

	// Create tracking client WITHOUT ownership config
	tc := NewClient(fakeClient)

	// Run cleanup - this should delete the resource since owner check is skipped
	err := tc.CleanupOrphans(ctx, testOwnerLabel, testOwnerValue, []schema.GroupVersionKind{configMapGVK})
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the ConfigMap was deleted (no owner check when ownership config is nil)
	var cm corev1.ConfigMap
	err = fakeClient.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "label-only-cm"}, &cm)
	g.Expect(errors.IsNotFound(err)).To(BeTrue(), "ConfigMap should be deleted - no ownership config means no owner check")
}

func setupScheme(g *WithT) *runtime.Scheme {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	g.Expect(err).NotTo(HaveOccurred())
	err = apiextensionsv1.AddToScheme(scheme)
	g.Expect(err).NotTo(HaveOccurred())
	return scheme
}

func TestIsCustomResourceDefinition(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns true for CustomResourceDefinition", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "applications.appstudio.redhat.com"},
		}
		g.Expect(kubernetes.IsCustomResourceDefinition(crd)).To(BeTrue())
	})

	t.Run("returns false for ConfigMap", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testNamespace,
			},
		}
		g.Expect(kubernetes.IsCustomResourceDefinition(cm)).To(BeFalse())
	})

	t.Run("returns false for empty GVK", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: testNamespace,
			},
		}
		// Clear TypeMeta so GVK is typically empty when not from scheme
		cm.APIVersion = ""
		cm.Kind = ""
		g.Expect(kubernetes.IsCustomResourceDefinition(cm)).To(BeFalse())
	})
}

func TestClient_SetOwnership_DoesNotSetControllerReferenceOnCRD(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	scheme := setupScheme(g)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Use a cluster-scoped owner (Namespace) so that owning a CRD would be valid if we set it.
	owner := &corev1.Namespace{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
		ObjectMeta: metav1.ObjectMeta{Name: "test-owner-namespace", UID: "owner-uid"},
	}
	err := fakeClient.Create(ctx, owner)
	g.Expect(err).NotTo(HaveOccurred())

	tc := NewClientWithOwnership(fakeClient, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})

	crd := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apiextensions.k8s.io/v1",
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "applications.appstudio.redhat.com",
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "appstudio.redhat.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: "Application", Plural: "applications"},
			Scope: apiextensionsv1.ClusterScoped,
		},
	}

	err = tc.SetOwnership(crd)
	g.Expect(err).NotTo(HaveOccurred())

	// Labels should be set
	g.Expect(crd.Labels).To(HaveKeyWithValue(testOwnerLabel, "test-owner-namespace"))
	g.Expect(crd.Labels).To(HaveKeyWithValue(testComponentLabel, testComponent))

	// Controller reference must NOT be set on CRDs so they are not cascade-deleted when the CR is removed.
	g.Expect(crd.OwnerReferences).To(BeEmpty())
}
