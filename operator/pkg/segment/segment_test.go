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

package segment

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	return scheme
}

func TestResolveWriteKey(t *testing.T) {
	t.Run("returns the CR key and source when the CR key is set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("cr-key", "")
		g.Expect(key).To(gomega.Equal("cr-key"))
		g.Expect(source).To(gomega.Equal("cr"))
	})

	t.Run("CR key takes precedence over the secret key when both are set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("cr-key", "secret-key")
		g.Expect(key).To(gomega.Equal("cr-key"))
		g.Expect(source).To(gomega.Equal("cr"))
	})

	t.Run("returns the secret key and source when the CR key is not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("", "secret-key")
		g.Expect(key).To(gomega.Equal("secret-key"))
		g.Expect(source).To(gomega.Equal("secret"))
	})

	t.Run("returns empty key and source when neither the CR key nor the secret key is set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("", "")
		g.Expect(key).To(gomega.BeEmpty())
		g.Expect(source).To(gomega.BeEmpty())
	})
}

func TestLogWriteKeyResolution(t *testing.T) {
	log := logr.Discard()

	t.Run("returns false when the key is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "", "")).To(gomega.BeFalse())
	})

	t.Run("returns true when the key is present", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "some-key", "cr")).To(gomega.BeTrue())
	})
}

func TestStripURLScheme(t *testing.T) {
	t.Run("strips https scheme from the default Segment API URL", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(StripURLScheme("https://api.segment.io/v1")).To(gomega.Equal("api.segment.io/v1"))
	})

	t.Run("strips https scheme from a custom proxy URL, preserving host and path", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(StripURLScheme("https://console.redhat.com/connections/api/v1")).
			To(gomega.Equal("console.redhat.com/connections/api/v1"))
	})

	t.Run("returns input unchanged when it cannot be parsed", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(StripURLScheme("://not a url")).To(gomega.Equal("://not a url"))
	})

	t.Run("returns input unchanged when there is no host", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(StripURLScheme("api.segment.io/v1")).To(gomega.Equal("api.segment.io/v1"))
	})
}

func TestResolveWriteKeySecretRef(t *testing.T) {
	ctx := context.Background()
	namespace := "segment-bridge"

	t.Run("returns empty string when ref is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
		key, err := ResolveWriteKeySecretRef(ctx, c, namespace, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(key).To(gomega.BeEmpty())
	})

	t.Run("returns the key when the Secret exists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vault-key", Namespace: namespace},
			Data:       map[string][]byte{"writeKey": []byte("secret-value")},
		}
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
		ref := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "vault-key"},
			Key:                  "writeKey",
		}
		key, err := ResolveWriteKeySecretRef(ctx, c, namespace, ref)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(key).To(gomega.Equal("secret-value"))
	})

	t.Run("returns empty string when optional Secret is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
		ref := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
			Key:                  "writeKey",
			Optional:             ptr.To(true),
		}
		key, err := ResolveWriteKeySecretRef(ctx, c, namespace, ref)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(key).To(gomega.BeEmpty())
	})

	t.Run("returns empty string when optional key is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vault-key", Namespace: namespace},
			Data:       map[string][]byte{"otherKey": []byte("value")},
		}
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
		ref := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "vault-key"},
			Key:                  "writeKey",
			Optional:             ptr.To(true),
		}
		key, err := ResolveWriteKeySecretRef(ctx, c, namespace, ref)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(key).To(gomega.BeEmpty())
	})

	t.Run("returns error when required Secret is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
		ref := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
			Key:                  "writeKey",
		}
		_, err := ResolveWriteKeySecretRef(ctx, c, namespace, ref)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get Secret"))
	})

	t.Run("returns error when required key is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "vault-key", Namespace: namespace},
			Data:       map[string][]byte{"otherKey": []byte("value")},
		}
		c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(secret).Build()
		ref := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "vault-key"},
			Key:                  "writeKey",
		}
		_, err := ResolveWriteKeySecretRef(ctx, c, namespace, ref)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring(`key "writeKey" not found`))
	})
}
