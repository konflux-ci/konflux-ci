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

package hashedconfigmap

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateHashSuffix(t *testing.T) {
	t.Run("generates consistent hash for same content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		content := "test content"
		hash1 := GenerateHashSuffix(content)
		hash2 := GenerateHashSuffix(content)
		g.Expect(hash1).To(gomega.Equal(hash2))
	})

	t.Run("generates different hash for different content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash1 := GenerateHashSuffix("content1")
		hash2 := GenerateHashSuffix("content2")
		g.Expect(hash1).NotTo(gomega.Equal(hash2))
	})

	t.Run("hash has correct length", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash := GenerateHashSuffix("any content")
		g.Expect(hash).To(gomega.HaveLen(HashSuffixLength))
	})

	t.Run("hash is hex encoded", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash := GenerateHashSuffix("test")
		g.Expect(hash).To(gomega.MatchRegexp("^[0-9a-f]+$"))
	})
}

func TestBuildConfigMapName(t *testing.T) {
	t.Run("combines base name and hash suffix", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name := BuildConfigMapName("my-config", "test content")
		g.Expect(name).To(gomega.HavePrefix("my-config-"))
		g.Expect(name).To(gomega.HaveLen(len("my-config-") + HashSuffixLength))
	})

	t.Run("consistent names for same content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name1 := BuildConfigMapName("config", "content")
		name2 := BuildConfigMapName("config", "content")
		g.Expect(name1).To(gomega.Equal(name2))
	})

	t.Run("different names for different content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name1 := BuildConfigMapName("config", "content1")
		name2 := BuildConfigMapName("config", "content2")
		g.Expect(name1).NotTo(gomega.Equal(name2))
	})
}

func newTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return scheme
}

func newFakeClient(scheme *runtime.Scheme, objs ...client.Object) client.Client {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
}

// newOwner creates a fake owner object for testing.
// Using a ConfigMap as owner since it's a simple namespaced resource.
func newOwner() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-owner",
			Namespace: testNamespace,
			UID:       types.UID("test-owner-uid"),
		},
	}
}

const (
	testBaseName     = "test-config"
	testNamespace    = "test-ns"
	testDataKey      = "config.yaml"
	testLabel        = "app.kubernetes.io/managed-by-test"
	testContent      = "key: value"
	testFieldManager = "test-controller"
)

func TestHashedConfigMapApply(t *testing.T) {
	const (
		baseName  = testBaseName
		namespace = testNamespace
		dataKey   = testDataKey
		label     = testLabel
	)

	t.Run("creates new ConfigMap with hashed name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()
		c := newFakeClient(scheme, owner)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		content := testContent

		result, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(result).NotTo(gomega.BeNil())
		g.Expect(result.ConfigMapName).To(gomega.HavePrefix(baseName + "-"))

		// Verify ConfigMap was created
		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: result.ConfigMapName, Namespace: namespace}, cm)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data[dataKey]).To(gomega.Equal(content))
		g.Expect(cm.Labels[label]).To(gomega.Equal("true"))
	})

	t.Run("sets owner reference", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()
		c := newFakeClient(scheme, owner)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		content := testContent

		result, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify owner reference was set
		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: result.ConfigMapName, Namespace: namespace}, cm)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.OwnerReferences).To(gomega.HaveLen(1))
		g.Expect(cm.OwnerReferences[0].Name).To(gomega.Equal(owner.Name))
		g.Expect(cm.OwnerReferences[0].UID).To(gomega.Equal(owner.UID))
	})

	t.Run("updates existing ConfigMap with same name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()
		content := testContent
		expectedName := BuildConfigMapName(baseName, content)

		// Pre-create the ConfigMap
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedName,
				Namespace: namespace,
				Labels:    map[string]string{label: "true"},
			},
			Data: map[string]string{dataKey: "old content"},
		}
		c := newFakeClient(scheme, owner, existingCM)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		result, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(result.ConfigMapName).To(gomega.Equal(expectedName))

		// Verify ConfigMap was updated
		cm := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: expectedName, Namespace: namespace}, cm)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data[dataKey]).To(gomega.Equal(content))
	})

	t.Run("cleans up old ConfigMaps with different hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()

		// Create an old ConfigMap with different hash
		oldCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      baseName + "-oldhash123",
				Namespace: namespace,
				Labels:    map[string]string{label: "true"},
			},
			Data: map[string]string{dataKey: "old content"},
		}
		c := newFakeClient(scheme, owner, oldCM)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		newContent := "new content"

		result, err := hcm.Apply(ctx, newContent, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify new ConfigMap was created
		newCM := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: result.ConfigMapName, Namespace: namespace}, newCM)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify old ConfigMap was deleted
		deletedCM := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: oldCM.Name, Namespace: namespace}, deletedCM)
		g.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
	})

	t.Run("does not delete ConfigMaps without managed label", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()

		// Create a ConfigMap with the prefix but without the managed label
		unmanagedCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      baseName + "-unmanaged1",
				Namespace: namespace,
				// No managed label
			},
			Data: map[string]string{dataKey: "unmanaged content"},
		}
		c := newFakeClient(scheme, owner, unmanagedCM)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		_, err := hcm.Apply(ctx, "new content", owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify unmanaged ConfigMap still exists
		existingCM := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: unmanagedCM.Name, Namespace: namespace}, existingCM)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("does not delete ConfigMaps with different base name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()

		// Create a ConfigMap with a different base name but with the managed label
		differentBaseCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "other-config-1234567890",
				Namespace: namespace,
				Labels:    map[string]string{label: "true"},
			},
			Data: map[string]string{dataKey: "other content"},
		}
		c := newFakeClient(scheme, owner, differentBaseCM)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		_, err := hcm.Apply(ctx, "new content", owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Verify the other ConfigMap still exists
		existingCM := &corev1.ConfigMap{}
		err = c.Get(ctx, client.ObjectKey{Name: differentBaseCM.Name, Namespace: namespace}, existingCM)
		g.Expect(err).NotTo(gomega.HaveOccurred())
	})

	t.Run("returns ConfigMap in result", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()
		c := newFakeClient(scheme, owner)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		content := testContent

		result, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(result.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(result.ConfigMap.Name).To(gomega.Equal(result.ConfigMapName))
		g.Expect(result.ConfigMap.Namespace).To(gomega.Equal(namespace))
		g.Expect(result.ConfigMap.Data[dataKey]).To(gomega.Equal(content))
	})
}

func TestHashedConfigMapApplyIdempotent(t *testing.T) {
	const (
		baseName  = testBaseName
		namespace = testNamespace
		dataKey   = testDataKey
		label     = testLabel
	)

	t.Run("apply is idempotent", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ctx := context.Background()
		scheme := newTestScheme()
		owner := newOwner()
		c := newFakeClient(scheme, owner)

		hcm := New(c, scheme, baseName, namespace, dataKey, label, testFieldManager)
		content := testContent

		// First apply
		result1, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Second apply with same content
		result2, err := hcm.Apply(ctx, content, owner)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// Names should be the same
		g.Expect(result1.ConfigMapName).To(gomega.Equal(result2.ConfigMapName))

		// Only one managed ConfigMap should exist (filter by label to exclude owner)
		cmList := &corev1.ConfigMapList{}
		err = c.List(ctx, cmList, client.InNamespace(namespace), client.MatchingLabels{label: "true"})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cmList.Items).To(gomega.HaveLen(1))
	})
}
