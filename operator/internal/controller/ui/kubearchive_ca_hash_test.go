/*
Copyright 2026 Konflux CI.

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

package ui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

func TestGetKubeArchiveCAHash(t *testing.T) {
	const configMapName = "kubearchive-ca-bundle"
	const dataKey = "ca-bundle.crt"
	caBundle := "first CA\nsecond CA\n"
	wantHash := fmt.Sprintf("%x", sha256.Sum256([]byte(caBundle)))[:10]

	tests := []struct {
		name       string
		configMaps []corev1.ConfigMap
		want       string
	}{
		{
			name: "hashes only the selected CA data",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: uiNamespace},
				Data:       map[string]string{dataKey: caBundle, "unrelated": "ignored"},
			}},
			want: wantHash,
		},
		{
			name: "hashes selected binary CA data",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: uiNamespace},
				BinaryData: map[string][]byte{dataKey: []byte(caBundle)},
			}},
			want: wantHash,
		},
		{
			name: "missing ConfigMap",
		},
		{
			name: "same name in another namespace",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: "other"},
				Data:       map[string]string{dataKey: caBundle},
			}},
		},
		{
			name: "different name in UI namespace",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: uiNamespace},
				Data:       map[string]string{dataKey: caBundle},
			}},
		},
		{
			name: "missing CA key",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: uiNamespace},
				Data:       map[string]string{"unrelated": caBundle},
			}},
		},
		{
			name: "empty CA key",
			configMaps: []corev1.ConfigMap{{
				ObjectMeta: metav1.ObjectMeta{Name: configMapName, Namespace: uiNamespace},
				Data:       map[string]string{dataKey: ""},
			}},
			want: fmt.Sprintf("%x", sha256.Sum256(nil))[:10],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("AddToScheme() error = %v", err)
			}
			objects := make([]client.Object, 0, len(tt.configMaps))
			for i := range tt.configMaps {
				objects = append(objects, &tt.configMaps[i])
			}
			reconciler := &KonfluxUIReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()}

			got, err := reconciler.getKubeArchiveCAHash(context.Background())
			if err != nil {
				t.Fatalf("getKubeArchiveCAHash() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("getKubeArchiveCAHash() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("propagates API errors", func(t *testing.T) {
		wantErr := errors.New("ConfigMap lookup failed")
		reconciler := &KonfluxUIReconciler{Client: kubeArchiveCAGetErrorClient{err: wantErr}}
		got, err := reconciler.getKubeArchiveCAHash(context.Background())
		if !errors.Is(err, wantErr) {
			t.Errorf("getKubeArchiveCAHash() error = %v, want %v", err, wantErr)
		}
		if got != "" {
			t.Errorf("getKubeArchiveCAHash() = %q, want empty hash", got)
		}
	})
}

type kubeArchiveCAGetErrorClient struct {
	client.Client
	err error
}

func (c kubeArchiveCAGetErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if key == (client.ObjectKey{Name: "kubearchive-ca-bundle", Namespace: uiNamespace}) {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestKubeArchiveCAHashDeploymentAnnotation(t *testing.T) {
	tests := []struct {
		name           string
		deploymentName string
		hash           string
		wantAnnotation bool
	}{
		{name: "proxy receives hash", deploymentName: proxyDeploymentName, hash: "hash-a", wantAnnotation: true},
		{name: "proxy omits empty hash", deploymentName: proxyDeploymentName},
		{name: "Dex does not receive hash", deploymentName: dexDeploymentName, hash: "hash-a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := getUIDeployment(t, tt.deploymentName)
			ui := &konfluxv1alpha1.KonfluxUI{}
			if err := applyUIDeploymentCustomizations(deployment, ui, nil, "", "", tt.hash, testEndpoint); err != nil {
				t.Fatalf("applyUIDeploymentCustomizations() error = %v", err)
			}
			got, present := deployment.Spec.Template.Annotations[kubeArchiveCAHashAnnotation]
			if present != tt.wantAnnotation {
				t.Fatalf("proxy hash annotation present = %t, want %t", present, tt.wantAnnotation)
			}
			if tt.wantAnnotation && got != tt.hash {
				t.Errorf("proxy hash annotation = %q, want %q", got, tt.hash)
			}
		})
	}
}

func TestMapKubeArchiveCAConfigMapToUI(t *testing.T) {
	reconciler := &KonfluxUIReconciler{}
	tests := []struct {
		name        string
		object      client.Object
		wantRequest bool
	}{
		{
			name:        "selected ConfigMap",
			object:      &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kubearchive-ca-bundle", Namespace: uiNamespace}},
			wantRequest: true,
		},
		{
			name:   "same name in another namespace",
			object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "kubearchive-ca-bundle", Namespace: "other"}},
		},
		{
			name:   "other ConfigMap in UI namespace",
			object: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: uiNamespace}},
		},
		{
			name:   "different object kind",
			object: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "kubearchive-ca-bundle", Namespace: uiNamespace}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reconciler.mapKubeArchiveCAConfigMapToUI(context.Background(), tt.object)
			if !tt.wantRequest {
				if len(got) != 0 {
					t.Errorf("mapKubeArchiveCAConfigMapToUI() = %v, want no requests", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("mapKubeArchiveCAConfigMapToUI() returned %d requests, want 1", len(got))
			}
			want := types.NamespacedName{Name: CRName}
			if got[0].NamespacedName != want {
				t.Errorf("mapKubeArchiveCAConfigMapToUI() = %v, want %v", got[0].NamespacedName, want)
			}
		})
	}
}

var _ = Describe("KubeArchive CA ConfigMap rollout", Serial, func() {
	It("updates the proxy template when CA data appears, changes, and disappears", func(ctx context.Context) {
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxUIReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ObjectStore: objectStore,
		}).SetupWithManager(mgr)).To(Succeed())
		mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			cancel()
			waitForStop()
		})

		ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
		Expect(k8sClient.Create(ctx, ui)).To(Succeed())
		DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

		deploymentKey := types.NamespacedName{Name: proxyDeploymentName, Namespace: uiNamespace}
		caConfigMapKey := types.NamespacedName{Name: "kubearchive-ca-bundle", Namespace: uiNamespace}
		readAnnotation := func(g Gomega) (string, bool) {
			deployment := &appsv1.Deployment{}
			g.Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
			value, present := deployment.Spec.Template.Annotations[kubeArchiveCAHashAnnotation]
			return value, present
		}

		By("starting without a CA hash")
		Eventually(func(g Gomega) {
			_, present := readAnnotation(g)
			g.Expect(present).To(BeFalse())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		caConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: caConfigMapKey.Name, Namespace: caConfigMapKey.Namespace},
			Data:       map[string]string{"ca-bundle.crt": "root-a"},
		}
		Expect(k8sClient.Create(ctx, caConfigMap)).To(Succeed())
		DeferCleanup(testutil.DeleteAndWait, k8sClient, caConfigMap)
		firstHash := fmt.Sprintf("%x", sha256.Sum256([]byte("root-a")))[:10]

		By("rolling the proxy when the CA bundle arrives")
		Eventually(func(g Gomega) {
			value, present := readAnnotation(g)
			g.Expect(present).To(BeTrue())
			g.Expect(value).To(Equal(firstHash))
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		Expect(k8sClient.Get(ctx, caConfigMapKey, caConfigMap)).To(Succeed())
		caConfigMap.Data["unrelated"] = "value"
		Expect(k8sClient.Update(ctx, caConfigMap)).To(Succeed())
		Consistently(func(g Gomega) {
			value, present := readAnnotation(g)
			g.Expect(present).To(BeTrue())
			g.Expect(value).To(Equal(firstHash))
		}).WithTimeout(2 * testutil.EventuallyPolling).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		Expect(k8sClient.Get(ctx, caConfigMapKey, caConfigMap)).To(Succeed())
		caConfigMap.Data["ca-bundle.crt"] = "root-b"
		Expect(k8sClient.Update(ctx, caConfigMap)).To(Succeed())
		secondHash := fmt.Sprintf("%x", sha256.Sum256([]byte("root-b")))[:10]

		By("rolling the proxy when the CA data changes")
		Eventually(func(g Gomega) {
			value, present := readAnnotation(g)
			g.Expect(present).To(BeTrue())
			g.Expect(value).To(Equal(secondHash))
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		Expect(k8sClient.Delete(ctx, caConfigMap)).To(Succeed())
		By("removing the hash after the CA bundle is deleted")
		Eventually(func(g Gomega) {
			_, present := readAnnotation(g)
			g.Expect(present).To(BeFalse())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	})

	It("preserves the applied proxy template when CA lookup fails", func(ctx context.Context) {
		ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
		Expect(k8sClient.Create(ctx, ui)).To(Succeed())
		DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

		reconciler := &KonfluxUIReconciler{
			Client:      k8sClient,
			Scheme:      k8sClient.Scheme(),
			ObjectStore: objectStore,
		}
		request := ctrl.Request{NamespacedName: types.NamespacedName{Name: CRName}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		caConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "kubearchive-ca-bundle", Namespace: uiNamespace},
			Data:       map[string]string{"ca-bundle.crt": "root-a"},
		}
		Expect(k8sClient.Create(ctx, caConfigMap)).To(Succeed())
		DeferCleanup(testutil.DeleteAndWait, k8sClient, caConfigMap)
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		deploymentKey := types.NamespacedName{Name: proxyDeploymentName, Namespace: uiNamespace}
		before := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, deploymentKey, before)).To(Succeed())
		Expect(before.Spec.Template.Annotations).To(HaveKey(kubeArchiveCAHashAnnotation))

		lookupErr := errors.New("CA ConfigMap API unavailable")
		reconciler.Client = kubeArchiveCAGetErrorClient{Client: k8sClient, err: lookupErr}
		_, err = reconciler.Reconcile(ctx, request)
		Expect(errors.Is(err, lookupErr)).To(BeTrue(), "reconcile error: %v", err)

		after := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, deploymentKey, after)).To(Succeed())
		Expect(after.Spec.Template).To(Equal(before.Spec.Template))
	})
})
