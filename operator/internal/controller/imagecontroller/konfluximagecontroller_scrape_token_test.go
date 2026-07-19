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

package imagecontroller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	testclock "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

type fakeTokenCreator struct {
	token     string
	expiresAt time.Time
	calls     int
}

func (f *fakeTokenCreator) CreateScraperToken(
	_ context.Context,
	_ types.NamespacedName,
	_ time.Duration,
) (string, time.Time, error) {
	f.calls++
	return f.token, f.expiresAt, nil
}

func testImageControllerWithComponentMetrics(metrics *konfluxv1alpha1.ComponentMetricsConfig) *konfluxv1alpha1.KonfluxImageController {
	return &konfluxv1alpha1.KonfluxImageController{
		ObjectMeta: metav1.ObjectMeta{Name: CRName},
		Spec: konfluxv1alpha1.KonfluxImageControllerSpec{
			ComponentMetrics: metrics,
		},
	}
}

var _ = Describe("Prometheus scrape token", func() {
	BeforeEach(func(ctx context.Context) {
		_ = kubernetes.IgnoreScrapeTokenNotFound(k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: imageControllerNamespace,
			},
		}))
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &konfluxv1alpha1.KonfluxImageController{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
		}))
	})

	startManagerWithTokenCreator := func(tokenCreator kubernetes.TokenCreator, clk clock.Clock) {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		reconciler := &KonfluxImageControllerReconciler{
			Client:       mgr.GetClient(),
			Scheme:       mgr.GetScheme(),
			ObjectStore:  objectStore,
			ClusterInfo:  nil,
			TokenCreator: tokenCreator,
			SecretReader: mgr.GetAPIReader(),
		}
		if clk != nil {
			reconciler.Clock = clk
		}
		Expect(reconciler.SetupWithManager(mgr)).To(Succeed())
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			mgrCancel()
			waitForStop()
		})
	}

	Context("when component metrics are enabled", func() {
		BeforeEach(func(ctx context.Context) {
			testutil.EnsureMetricsTLSSecrets(ctx, k8sClient, imageControllerNamespace)
		})

		It("creates prometheus-scrape-token and wires ServiceMonitor bearerTokenSecret", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeTokenCreator{
				token:     "envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			imageController := testImageControllerWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: imageControllerNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("envtest-scrape-token")))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "ServiceMonitor",
			})
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "image-controller",
					Namespace: imageControllerNamespace,
				}, sm)).To(Succeed())
				endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(endpoints).NotTo(BeEmpty())
				ep, ok := endpoints[0].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				secretRef, found, err := unstructured.NestedMap(ep, "bearerTokenSecret")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(secretRef["name"]).To(Equal(kubernetes.ScrapeTokenSecretName))
				testutil.ExpectVerifiedMetricsEndpointTLS(g, ep,
					"image-controller-controller-manager-metrics-service.image-controller.svc")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates prometheus-scrape-token when deleted", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeTokenCreator{
				token:     "envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			imageController := testImageControllerWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: imageControllerNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretNN, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretNN.Name, Namespace: secretNN.Namespace},
			})).To(Succeed())

			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("envtest-scrape-token")))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("refreshes the scrape token after the rotation threshold", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeTokenCreator{
				token:     "first-token",
				expiresAt: clk.Now().Add(time.Hour),
			}

			mgr := testutil.NewTestManager(testEnv)
			reconciler := &KonfluxImageControllerReconciler{
				Client:       k8sClient,
				Scheme:       mgr.GetScheme(),
				ObjectStore:  objectStore,
				ClusterInfo:  nil,
				TokenCreator: fake,
				Clock:        clk,
			}

			imageController := testImageControllerWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: CRName}})
			Expect(err).NotTo(HaveOccurred())
			Expect(fake.calls).To(Equal(1))

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: imageControllerNamespace,
			}
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
			Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("first-token")))

			clk.SetTime(clk.Now().Add(40 * time.Minute))
			fake.token = "rotated-token"
			fake.expiresAt = clk.Now().Add(time.Hour)

			_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: CRName}})
			Expect(err).NotTo(HaveOccurred())
			Expect(fake.calls).To(Equal(2))

			Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
			Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("rotated-token")))
		})
	})

	Context("when metrics TLS secrets are missing", func() {
		BeforeEach(func(ctx context.Context) {
			testutil.DeleteMetricsTLSSecrets(ctx, k8sClient, imageControllerNamespace)
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor",
			})
			sm.SetNamespace(imageControllerNamespace)
			sm.SetName("image-controller")
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, sm))).To(Succeed())
		})

		It("mints the scrape token but defers ServiceMonitor apply", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeTokenCreator{
				token:     "envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			imageController := testImageControllerWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: imageControllerNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("envtest-scrape-token")))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "ServiceMonitor",
			})
			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "image-controller",
					Namespace: imageControllerNamespace,
				}, sm)
				g.Expect(client.IgnoreNotFound(err)).To(Succeed())
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(Succeed())
		})
	})

	Context("when component metrics are disabled", func() {
		It("skips ServiceMonitor apply and scrape token reconciliation", func(ctx context.Context) {
			disabled := false
			mgr := testutil.NewTestManager(testEnv)
			fake := &fakeTokenCreator{token: "unused", expiresAt: time.Now().Add(time.Hour)}
			reconciler := &KonfluxImageControllerReconciler{
				Client:       k8sClient,
				Scheme:       mgr.GetScheme(),
				ObjectStore:  objectStore,
				TokenCreator: fake,
			}

			imageController := testImageControllerWithComponentMetrics(&konfluxv1alpha1.ComponentMetricsConfig{
				Enabled: &disabled,
			})
			Expect(k8sClient.Create(ctx, imageController)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, imageController)

			_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: CRName}})
			Expect(err).NotTo(HaveOccurred())
			Expect(fake.calls).To(Equal(0))

			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "ServiceMonitor",
			})
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "image-controller",
				Namespace: imageControllerNamespace,
			}, sm)
			Expect(client.IgnoreNotFound(err)).To(Succeed())
		})
	})
})
