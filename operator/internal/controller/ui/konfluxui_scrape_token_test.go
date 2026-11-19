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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// fakeUITokenCreator is a test-local implementation of kubernetes.TokenCreator.
type fakeUITokenCreator struct {
	token     string
	expiresAt time.Time
	calls     int
	err       error
}

func (f *fakeUITokenCreator) CreateScraperToken(
	_ context.Context,
	_ types.NamespacedName,
	_ time.Duration,
) (string, time.Time, error) {
	f.calls++
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	return f.token, f.expiresAt, nil
}

const (
	uiMetricsReaderCRBName = "prometheus-konflux-ui-proxy-metrics-reader"
)

var uiClusterScopedMetricsChildren = []client.Object{
	&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: uiMetricsReaderCRBName}},
	&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "konflux-ui-proxy-metrics-reader"}},
	&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "dex"}},
	&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "dex"}},
	&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "konflux-proxy"}},
	&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "konflux-proxy"}},
	&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "konflux-proxy-namespace-lister"}},
	&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "konflux-proxy-namespace-lister"}},
}

func testUIWithComponentMetrics(metrics *konfluxv1alpha1.ComponentMetricsConfig) *konfluxv1alpha1.KonfluxUI {
	return &konfluxv1alpha1.KonfluxUI{
		ObjectMeta: metav1.ObjectMeta{Name: CRName},
		Spec: konfluxv1alpha1.KonfluxUISpec{
			ComponentMetrics: metrics,
		},
	}
}

var _ = Describe("UI Prometheus scrape token", func() {
	startManagerWithTokenCreator := func(tokenCreator kubernetes.TokenCreator, clk clock.Clock) chan event.TypedGenericEvent[client.Object] {
		mgrCtx, mgrCancel := context.WithCancel(testEnv.Ctx)
		mgr := testutil.NewTestManager(testEnv)
		rotationEvents := make(chan event.TypedGenericEvent[client.Object], 1)
		reconciler := &KonfluxUIReconciler{
			Client:              mgr.GetClient(),
			Scheme:              mgr.GetScheme(),
			ObjectStore:         objectStore,
			ClusterInfo:         nil,
			TokenCreator:        tokenCreator,
			SecretReader:        mgr.GetAPIReader(),
			TokenRotationEvents: rotationEvents,
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
		return rotationEvents
	}

	BeforeEach(func(ctx context.Context) {
		_ = kubernetes.IgnoreScrapeTokenNotFound(k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: uiNamespace,
			},
		}))
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
		}))
		sm := &unstructured.Unstructured{}
		sm.SetGroupVersionKind(schema.GroupVersionKind{
			Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor",
		})
		sm.SetNamespace(uiNamespace)
		sm.SetName(uiServiceMonitorName)
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, sm))
		_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      kubernetes.MetricsScraperServiceAccountName,
				Namespace: uiNamespace,
			},
		}))
	})

	Context("when component metrics are enabled", func() {
		BeforeEach(func(ctx context.Context) {
			testutil.EnsureNamedMetricsTLSSecret(ctx, k8sClient, uiNamespace, uiTLSSecretName)
		})

		It("creates prometheus-scrape-token and wires ServiceMonitor bearerTokenSecret", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeUITokenCreator{
				token:     "ui-envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			rotationEvents := startManagerWithTokenCreator(fake, clk)

			ui := testUIWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, uiClusterScopedMetricsChildren...)

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: uiNamespace,
			}
			By("verifying prometheus-scrape-token is created")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("ui-envtest-scrape-token")))
				g.Expect(secret.Annotations).To(HaveKeyWithValue(
					"konflux.konflux-ci.dev/scrape-token-expires-at",
					clk.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying ServiceMonitor is created with HTTPS bearerTokenSecret and tlsConfig")
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "monitoring.coreos.com",
				Version: "v1",
				Kind:    "ServiceMonitor",
			})
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      uiServiceMonitorName,
					Namespace: uiNamespace,
				}, sm)).To(Succeed())
				endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(endpoints).NotTo(BeEmpty())
				ep, ok := endpoints[0].(map[string]interface{})
				g.Expect(ok).To(BeTrue())
				// scheme must be https
				g.Expect(ep["scheme"]).To(Equal("https"))
				// bearerTokenSecret must be wired
				secretRef, found, err := unstructured.NestedMap(ep, "bearerTokenSecret")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(secretRef["name"]).To(Equal(kubernetes.ScrapeTokenSecretName))
				g.Expect(secretRef["key"]).To(Equal(kubernetes.ScrapeTokenSecretKey))
				_, hasFile := ep["bearerTokenFile"]
				g.Expect(hasFile).To(BeFalse())
				// tlsConfig must reference serving-cert CA for verified TLS
				testutil.ExpectVerifiedNamedMetricsEndpointTLS(g, ep,
					"proxy.konflux-ui.svc", uiTLSSecretName)
				g.Expect(sm.GetAnnotations()).To(HaveKey(kubernetes.ServiceMonitorResyncAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying metrics-scraper ServiceAccount and CRB subjects")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      kubernetes.MetricsScraperServiceAccountName,
					Namespace: uiNamespace,
				}, &corev1.ServiceAccount{})).To(Succeed())
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: uiMetricsReaderCRBName,
				}, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(Equal([]rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      kubernetes.MetricsScraperServiceAccountName,
					Namespace: uiNamespace,
				}}))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("exercising the token rotation event watch source")
			clk.SetTime(clk.Now().Add(40 * time.Minute))
			fake.token = "ui-after-rotation-event"
			fake.expiresAt = clk.Now().Add(time.Hour)
			rotationEvents <- event.TypedGenericEvent[client.Object]{Object: &corev1.Secret{}}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, secretNN, secret)).To(Succeed())
				g.Expect(secret.Data[kubernetes.ScrapeTokenSecretKey]).To(Equal([]byte("ui-after-rotation-event")))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates prometheus-scrape-token when deleted", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeUITokenCreator{
				token:     "ui-envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			ui := testUIWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, uiClusterScopedMetricsChildren...)

			secretNN := types.NamespacedName{
				Name:      kubernetes.ScrapeTokenSecretName,
				Namespace: uiNamespace,
			}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretNN, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the secret")
			Expect(k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: kubernetes.ScrapeTokenSecretName, Namespace: uiNamespace,
			}})).To(Succeed())

			By("verifying the secret is recreated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretNN, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("holds off applying ServiceMonitor when TLS secret is absent", func(ctx context.Context) {
			testutil.DeleteNamedMetricsTLSSecret(ctx, k8sClient, uiNamespace, uiTLSSecretName)

			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeUITokenCreator{
				token:     "ui-envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			ui := testUIWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, uiClusterScopedMetricsChildren...)

			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(schema.GroupVersionKind{
				Group: "monitoring.coreos.com", Version: "v1", Kind: "ServiceMonitor",
			})
			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      uiServiceMonitorName,
					Namespace: uiNamespace,
				}, sm)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			By("creating the TLS secret — ServiceMonitor should appear")
			testutil.EnsureNamedMetricsTLSSecret(ctx, k8sClient, uiNamespace, uiTLSSecretName)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      uiServiceMonitorName,
					Namespace: uiNamespace,
				}, sm)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("reports error when TokenCreator fails", func(ctx context.Context) {
			fake := &fakeUITokenCreator{err: fmt.Errorf("token mint failed")}
			startManagerWithTokenCreator(fake, nil)

			ui := testUIWithComponentMetrics(testutil.DefaultComponentMetricsConfig())
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, uiClusterScopedMetricsChildren...)

			Eventually(func(g Gomega) {
				updated := &konfluxv1alpha1.KonfluxUI{}
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(ui), updated)).To(Succeed())
				found := false
				for _, c := range updated.Status.Conditions {
					if c.Type == "Ready" && c.Status == metav1.ConditionFalse {
						found = true
						break
					}
				}
				g.Expect(found).To(BeTrue(), "expected Ready=False condition after TokenCreator failure")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("when component metrics are disabled", func() {
		It("does not create prometheus-scrape-token", func(ctx context.Context) {
			clk := testclock.NewFakeClock(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
			fake := &fakeUITokenCreator{
				token:     "ui-envtest-scrape-token",
				expiresAt: clk.Now().Add(time.Hour),
			}
			startManagerWithTokenCreator(fake, clk)

			ui := testUIWithComponentMetrics(&konfluxv1alpha1.ComponentMetricsConfig{Enabled: boolPtr(false)})
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
			})

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      proxyDeploymentName,
					Namespace: uiNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Consistently(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name: kubernetes.ScrapeTokenSecretName, Namespace: uiNamespace,
				}, &corev1.Secret{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})

// boolPtr is a helper to get a pointer to a bool literal.
func boolPtr(b bool) *bool { return &b }
