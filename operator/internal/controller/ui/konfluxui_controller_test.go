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

package ui

import (
	"context"
	"encoding/base64"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	consolev1 "github.com/openshift/api/console/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/segmentbridge"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/consolelink"
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	"github.com/konflux-ci/konflux-ci/operator/pkg/hashedsecret"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	proxyServiceAccountName   = "proxy"
	dexClusterRoleName        = "dex"
	dexClusterRoleBindingName = "dex"
	servingCertificateName    = "serving-cert"
	caIssuerName              = "ui-ca-issuer"
)

func noDefaultSegmentKey() string             { return "" }
func staticSegmentKey(k string) func() string { return func() string { return k } }

var _ = Describe("KonfluxUI Controller", func() {
	// startManager creates a per-test manager with the given reconciler configuration
	// and registers a DeferCleanup to cancel it after the test.
	// A per-test manager is required because each test wires the reconciler with a different
	// GetDefaultSegmentKey or ClusterInfo, and a shared suite-level manager cannot be
	// re-configured between tests.
	startManager := func(getDefaultSegmentKey func() string, clusterInfo *clusterinfo.Info) {
		mgr := testutil.NewTestManager(testEnv)
		Expect((&KonfluxUIReconciler{
			Client:               mgr.GetClient(),
			Scheme:               mgr.GetScheme(),
			ObjectStore:          objectStore,
			GetDefaultSegmentKey: getDefaultSegmentKey,
			ClusterInfo:          clusterInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
		waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
		DeferCleanup(func() {
			cancel()
			waitForStop()
		})
	}

	// waitForReconcile blocks until both UI Deployments exist, proving the initial
	// reconcile has completed. Used as a sentinel before synchronous absence checks.
	waitForReconcile := func(ctx context.Context) {
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: proxyDeploymentName, Namespace: uiNamespace,
			}, &appsv1.Deployment{})).To(Succeed())
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: dexDeploymentName, Namespace: uiNamespace,
			}, &appsv1.Deployment{})).To(Succeed())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	}

	Context("When reconciling a resource", func() {
		It("should successfully reconcile the resource", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec:       konfluxv1alpha1.KonfluxUISpec{},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})
			// Wait for the Deployment rather than Ready=True: UpdateComponentStatuses
			// gates Ready=True on ReadyReplicas == Replicas, which never happens in
			// envtest (no kubelet → pods never start).
			waitForReconcile(ctx)
		})
	})

	Context("ensureUISecrets", func() {
		cleanupSecrets := func(ctx context.Context) {
			_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
			}})
			_ = k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: oauth2ProxyCookieSecretName, Namespace: uiNamespace,
			}})
		}

		BeforeEach(func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			By("ensuring the UI namespace exists")
			uiNs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: uiNamespace}}
			err := k8sClient.Create(ctx, uiNs)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			By("pre-cleaning any leftover secrets")
			cleanupSecrets(ctx)
		})

		It("Should preserve existing secret data when secret already exists with valid data", func(ctx context.Context) {
			By("creating the secret with existing data before the CR exists")
			existingData := []byte("existing-secret-value")
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				},
				Data: map[string][]byte{"client-secret": existingData},
			})).To(Succeed())

			By("creating the KonfluxUI CR to trigger reconcile")
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			By("verifying the secret data was preserved after reconcile")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				// Owner reference being set proves reconcile ran
				g.Expect(secret.OwnerReferences).NotTo(BeEmpty())
				g.Expect(secret.Data["client-secret"]).To(Equal(existingData))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should update ownership labels and owner reference when secret exists but has incorrect ownership", func(ctx context.Context) {
			By("creating the secret with wrong ownership before the CR exists")
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      oauth2ProxyClientSecretName,
					Namespace: uiNamespace,
					Labels: map[string]string{
						constant.KonfluxOwnerLabel:     "wrong-owner",
						constant.KonfluxComponentLabel: "wrong-component",
					},
				},
				Data: map[string][]byte{"client-secret": []byte("existing-value")},
			})).To(Succeed())

			By("creating the KonfluxUI CR to trigger reconcile")
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			By("verifying the ownership labels and owner reference were corrected")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(secret.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
				g.Expect(secret.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.UI)))
				g.Expect(secret.OwnerReferences).To(HaveLen(1))
				g.Expect(secret.OwnerReferences[0].Name).To(Equal(CRName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should regenerate secret data when secret exists but has empty data", func(ctx context.Context) {
			By("creating the secret with empty data before the CR exists")
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				},
				Data: map[string][]byte{},
			})).To(Succeed())

			By("creating the KonfluxUI CR to trigger reconcile")
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			By("verifying the secret data was regenerated")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(secret.Data).To(HaveKey("client-secret"))
				g.Expect(secret.Data["client-secret"]).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use URL-safe base64 encoding for client-secret", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				clientSecretValue := string(secret.Data["client-secret"])
				g.Expect(clientSecretValue).NotTo(BeEmpty())
				g.Expect(clientSecretValue).NotTo(ContainSubstring("="), "URL-safe base64 should have no padding")
				g.Expect(clientSecretValue).To(MatchRegexp("^[A-Za-z0-9_-]+$"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use standard base64 encoding for cookie-secret", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyCookieSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				cookieSecretValue := string(secret.Data["cookie-secret"])
				g.Expect(cookieSecretValue).NotTo(BeEmpty())
				g.Expect(cookieSecretValue).To(MatchRegexp("^[A-Za-z0-9+/]+=*$"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create both secrets in a single reconcile", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, &corev1.Secret{})).To(Succeed())
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyCookieSecretName, Namespace: uiNamespace,
				}, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should be idempotent across multiple reconciles", func(ctx context.Context) {
			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
				cleanupSecrets(ctx)
			})

			By("waiting for the client-secret to be created")
			var firstValue []byte
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(secret.Data["client-secret"]).NotTo(BeEmpty())
				firstValue = secret.Data["client-secret"]
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the value is stable across subsequent reconciles")
			Consistently(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: oauth2ProxyClientSecretName, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(secret.Data["client-secret"]).To(Equal(firstValue))
			}, 5*time.Second, time.Second).Should(Succeed())
		})
	})

	Context("generateRandomBytes", func() {
		It("Should generate valid URL-safe base64 encoded bytes", func() {
			By("generating random bytes with URL-safe encoding")
			result, err := generateRandomBytes(20, true)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeEmpty())

			By("verifying it uses URL-safe base64 encoding (no padding)")
			resultStr := string(result)
			Expect(resultStr).NotTo(ContainSubstring("="))
			Expect(resultStr).To(MatchRegexp("^[A-Za-z0-9_-]+$"))

			By("verifying it can be decoded back to original length")
			decoded, err := base64.RawURLEncoding.DecodeString(resultStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(decoded).To(HaveLen(20))
		})

		It("Should generate valid standard base64 encoded bytes", func() {
			By("generating random bytes with standard encoding")
			result, err := generateRandomBytes(16, false)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeEmpty())

			By("verifying it uses standard base64 encoding")
			resultStr := string(result)
			Expect(resultStr).To(MatchRegexp("^[A-Za-z0-9+/]+=*$"))

			By("verifying it can be decoded back to original length")
			decoded, err := base64.StdEncoding.DecodeString(resultStr)
			Expect(err).NotTo(HaveOccurred())
			Expect(decoded).To(HaveLen(16))
		})

		It("Should generate different random values on each call", func() {
			By("generating first random value")
			result1, err := generateRandomBytes(20, true)
			Expect(err).NotTo(HaveOccurred())

			By("generating second random value")
			result2, err := generateRandomBytes(20, true)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the values are different")
			Expect(result1).NotTo(Equal(result2))
		})

		It("Should generate longer encoded output for longer input", func() {
			By("generating with small length")
			small, err := generateRandomBytes(8, true)
			Expect(err).NotTo(HaveOccurred())

			By("generating with larger length")
			large, err := generateRandomBytes(32, true)
			Expect(err).NotTo(HaveOccurred())

			By("verifying larger input produces longer output")
			Expect(len(large)).To(BeNumerically(">", len(small)))
		})

		It("Should handle zero length input", func() {
			By("generating with zero length")
			result, err := generateRandomBytes(0, true)
			Expect(err).NotTo(HaveOccurred())

			By("verifying result is empty")
			Expect(result).To(BeEmpty())
		})
	})

	Context("Ingress reconciliation via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI

		// refreshUI re-fetches the CR to get the latest ResourceVersion before updates.
		refreshUI := func(ctx context.Context) {
			ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
		}

		// enableIngress updates the CR to enable ingress; the manager reconciles automatically.
		enableIngress := func(ctx context.Context, host string) {
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				Enabled: ptr.To(true),
				Host:    host,
			}
			ExpectWithOffset(1, k8sClient.Update(ctx, ui)).To(Succeed())
		}

		BeforeEach(func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			By("pre-cleaning any existing Ingress")
			_ = k8sClient.Delete(ctx, &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
				Name: ingress.IngressName, Namespace: uiNamespace,
			}})

			ui = &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
				_ = k8sClient.Delete(ctx, &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}})
			})
		})

		It("Should create Ingress when ingress is enabled", func(ctx context.Context) {
			enableIngress(ctx, "test.example.com")

			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules).To(HaveLen(1))
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should delete Ingress when ingress spec is set to nil", func(ctx context.Context) {
			By("enabling ingress to create it first")
			enableIngress(ctx, "test.example.com")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, &networkingv1.Ingress{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting ingress spec to nil")
			refreshUI(ctx)
			ui.Spec.Ingress = nil
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, &networkingv1.Ingress{}))).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should delete Ingress when Enabled is set to false", func(ctx context.Context) {
			By("enabling ingress to create it first")
			enableIngress(ctx, "test.example.com")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, &networkingv1.Ingress{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("setting Enabled to false")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, &networkingv1.Ingress{}))).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should not error when ingress is disabled and no Ingress exists", func(ctx context.Context) {
			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			By("verifying no Ingress was created")
			Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
				Name: ingress.IngressName, Namespace: uiNamespace,
			}, &networkingv1.Ingress{}))).To(BeTrue())
		})

		It("Should update Ingress when hostname changes", func(ctx context.Context) {
			By("enabling ingress with initial hostname")
			enableIngress(ctx, "initial.example.com")
			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("initial.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating to new hostname")
			refreshUI(ctx)
			ui.Spec.Ingress.Host = "updated.example.com"
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("updated.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should include OpenShift TLS annotations on created Ingress", func(ctx context.Context) {
			enableIngress(ctx, "test.example.com")

			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, ing)).To(Succeed())
				g.Expect(ing.Annotations).To(HaveKeyWithValue(
					"route.openshift.io/destination-ca-certificate-secret", "ui-ca"))
				g.Expect(ing.Annotations).To(HaveKeyWithValue(
					"route.openshift.io/termination", "reencrypt"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should set owner reference on created Ingress", func(ctx context.Context) {
			enableIngress(ctx, "test.example.com")

			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}, ing)).To(Succeed())
				g.Expect(ing.OwnerReferences).To(HaveLen(1))
				g.Expect(ing.OwnerReferences[0].Name).To(Equal(CRName))
				g.Expect(ing.OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should update KonfluxUI status with ingress information", func(ctx context.Context) {
			enableIngress(ctx, "status-test.example.com")

			Eventually(func(g Gomega) {
				updatedUI := &konfluxv1alpha1.KonfluxUI{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, updatedUI)).To(Succeed())
				g.Expect(updatedUI.Status.Ingress).NotTo(BeNil())
				g.Expect(updatedUI.Status.Ingress.Enabled).To(BeTrue())
				g.Expect(updatedUI.Status.Ingress.Hostname).To(Equal("status-test.example.com"))
				g.Expect(updatedUI.Status.Ingress.URL).To(Equal("https://status-test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("OpenShift OAuth reconciliation via Reconcile", Serial, func() {
		var openShiftClusterInfo *clusterinfo.Info
		var defaultClusterInfo *clusterinfo.Info

		BeforeEach(func(ctx context.Context) {
			By("pre-cleaning any existing OpenShift OAuth resources")
			_ = k8sClient.Delete(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Name: "dex", Namespace: uiNamespace,
			}})

			By("building cluster info for OpenShift and non-OpenShift platforms")
			var err error
			openShiftClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			defaultClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		// createCR creates the KonfluxUI CR with ingress enabled and registers DeferCleanup.
		createCR := func(ctx context.Context) *konfluxv1alpha1.KonfluxUI {
			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "openshift-test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
				_ = k8sClient.Delete(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
					Name: "dex", Namespace: uiNamespace,
				}})
			})
			return ui
		}

		It("Should create OpenShift OAuth resources when running on OpenShift (default behavior)", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			createCR(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations).To(HaveKeyWithValue(
					dex.OpenShiftRedirectURIAnnotation,
					"https://openshift-test.example.com/idp/callback",
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should NOT annotate dex ServiceAccount when NOT running on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, defaultClusterInfo)
			createCR(ctx)

			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations).NotTo(HaveKey(dex.OpenShiftRedirectURIAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should NOT create OpenShift OAuth resources when ClusterInfo is nil", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)
			createCR(ctx)

			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations).NotTo(HaveKey(dex.OpenShiftRedirectURIAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should NOT create OpenShift OAuth resources when explicitly disabled on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("disabling OpenShift login before first reconcile settles")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(false),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("waiting for reconcile to complete")
			waitForReconcile(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations).NotTo(HaveKey(dex.OpenShiftRedirectURIAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create OpenShift OAuth resources when explicitly enabled on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("explicitly enabling OpenShift login")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(true),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations).To(HaveKeyWithValue(
					dex.OpenShiftRedirectURIAnnotation,
					"https://openshift-test.example.com/idp/callback",
				))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should delete OpenShift OAuth resources when disabled after being enabled", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("waiting for OAuth resources to be created")
			Eventually(func(g Gomega) {
				sa := corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, &sa)).To(Succeed())
				g.Expect(sa.Annotations).To(HaveKey(dex.OpenShiftRedirectURIAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("disabling OpenShift login")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(false),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				sa := corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, &sa)).To(Succeed())
				g.Expect(sa.Annotations).NotTo(HaveKey(dex.OpenShiftRedirectURIAnnotation))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should set owner reference on OpenShift OAuth resources", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			createCR(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.OwnerReferences).To(HaveLen(1))
				g.Expect(sa.OwnerReferences[0].Name).To(Equal(CRName))
				g.Expect(sa.OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use correct redirect URI format without port", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			createCR(ctx)

			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: "dex", Namespace: uiNamespace,
				}, sa)).To(Succeed())
				g.Expect(sa.Annotations[dex.OpenShiftRedirectURIAnnotation]).To(
					Equal("https://openshift-test.example.com/idp/callback"),
				)
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("NodePort Service configuration via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI

		BeforeEach(func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui = &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
			})
		})

		getProxySvc := func(ctx context.Context, g Gomega) *corev1.Service {
			svc := &corev1.Service{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: proxyServiceName, Namespace: uiNamespace,
			}, svc)).To(Succeed())
			return svc
		}

		It("Should create proxy Service as ClusterIP by default", func(ctx context.Context) {
			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create proxy Service as NodePort when nodePortService is configured", func(ctx context.Context) {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should set specific HTTPS NodePort when httpsPort is specified", func(ctx context.Context) {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{
					HTTPSPort: ptr.To(int32(30443)),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				svc := getProxySvc(ctx, g)
				g.Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
				var httpsPort *corev1.ServicePort
				for i := range svc.Spec.Ports {
					if svc.Spec.Ports[i].Name == "web-tls" {
						httpsPort = &svc.Spec.Ports[i]
						break
					}
				}
				g.Expect(httpsPort).NotTo(BeNil())
				g.Expect(httpsPort.NodePort).To(Equal(int32(30443)))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should change proxy Service from ClusterIP to NodePort when nodePortService is added", func(ctx context.Context) {
			By("waiting for initial ClusterIP service")
			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("adding NodePort configuration")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{
					HTTPSPort: ptr.To(int32(30444)),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should change proxy Service from NodePort to ClusterIP when nodePortService is removed", func(ctx context.Context) {
			By("configuring NodePort service")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("removing NodePort configuration")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = nil
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(getProxySvc(ctx, g).Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("ConsoleLink reconciliation via Reconcile", Serial, func() {
		var openShiftClusterInfo *clusterinfo.Info
		var defaultClusterInfo *clusterinfo.Info

		BeforeEach(func(ctx context.Context) {
			By("pre-cleaning any existing ConsoleLink")
			_ = k8sClient.Delete(ctx, &consolev1.ConsoleLink{ObjectMeta: metav1.ObjectMeta{
				Name: consolelink.ConsoleLinkName,
			}})

			By("building cluster info for OpenShift and non-OpenShift platforms")
			var err error
			openShiftClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			defaultClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources:     map[string]*metav1.APIResourceList{},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		// createCR creates a KonfluxUI CR with ingress enabled and registers DeferCleanup.
		createCR := func(ctx context.Context) *konfluxv1alpha1.KonfluxUI {
			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "consolelink-test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
				_ = k8sClient.Delete(ctx, &consolev1.ConsoleLink{ObjectMeta: metav1.ObjectMeta{
					Name: consolelink.ConsoleLinkName,
				}})
			})
			return ui
		}

		It("Should create ConsoleLink when ingress is enabled and running on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			createCR(ctx)

			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-test.example.com"))
				g.Expect(cl.Spec.Text).To(Equal("Konflux Console"))
				g.Expect(cl.Spec.Location).To(Equal(consolev1.ApplicationMenu))
				g.Expect(cl.Spec.ApplicationMenu).NotTo(BeNil())
				g.Expect(cl.Spec.ApplicationMenu.Section).To(Equal("Konflux"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should NOT create ConsoleLink when NOT running on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, defaultClusterInfo)
			createCR(ctx)

			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
				Name: consolelink.ConsoleLinkName,
			}, &consolev1.ConsoleLink{}))).To(BeTrue())
		})

		It("Should NOT create ConsoleLink when ClusterInfo is nil", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)
			createCR(ctx)

			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
				Name: consolelink.ConsoleLinkName,
			}, &consolev1.ConsoleLink{}))).To(BeTrue())
		})

		It("Should NOT create ConsoleLink when ingress is disabled on OpenShift", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("disabling ingress")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, &consolev1.ConsoleLink{}))).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should delete ConsoleLink when ingress is disabled", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("waiting for ConsoleLink to be created")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, &consolev1.ConsoleLink{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("disabling ingress")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, &consolev1.ConsoleLink{}))).To(BeTrue())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should update ConsoleLink when hostname changes", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			ui := createCR(ctx)

			By("waiting for initial ConsoleLink")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("updating the hostname")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: CRName}, ui)).To(Succeed())
			ui.Spec.Ingress.Host = "updated-consolelink.example.com"
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://updated-consolelink.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ConsoleLink when deleted while ingress is enabled", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, openShiftClusterInfo)
			createCR(ctx)

			By("waiting for initial ConsoleLink creation")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ConsoleLink externally")
			Expect(k8sClient.Delete(ctx, &consolev1.ConsoleLink{
				ObjectMeta: metav1.ObjectMeta{Name: consolelink.ConsoleLinkName},
			})).To(Succeed())

			By("verifying the ConsoleLink is recreated with correct spec")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: consolelink.ConsoleLinkName,
				}, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-test.example.com"))
				g.Expect(cl.Spec.Text).To(Equal("Konflux Console"))
				g.Expect(cl.Spec.Location).To(Equal(consolev1.ApplicationMenu))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Self-healing", func() {
		It("recreates Deployment when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			deploymentNN := types.NamespacedName{Name: proxyDeploymentName, Namespace: uiNamespace}

			By("waiting for initial Deployment creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, deploymentNN, &appsv1.Deployment{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Deployment")
			Expect(k8sClient.Delete(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deploymentNN.Name, Namespace: deploymentNN.Namespace},
			})).To(Succeed())

			By("verifying the Deployment is recreated with correct spec")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				g.Expect(dep.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, reverseProxyContainerName)
				g.Expect(container).NotTo(BeNil(), "reverse-proxy container should exist")
				g.Expect(container.Image).NotTo(BeEmpty(), "container image should be set")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Service when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			svcNN := types.NamespacedName{Name: proxyServiceName, Namespace: uiNamespace}

			By("waiting for initial Service creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, svcNN, &corev1.Service{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Service")
			Expect(k8sClient.Delete(ctx, &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: svcNN.Name, Namespace: svcNN.Namespace},
			})).To(Succeed())

			By("verifying the Service is recreated with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ServiceAccount when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			saNN := types.NamespacedName{Name: proxyServiceAccountName, Namespace: uiNamespace}

			By("waiting for initial ServiceAccount creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, saNN, &corev1.ServiceAccount{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ServiceAccount")
			Expect(k8sClient.Delete(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: saNN.Name, Namespace: saNN.Namespace},
			})).To(Succeed())

			By("verifying the ServiceAccount is recreated with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRole when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: dexClusterRoleName}})

			crNN := types.NamespacedName{Name: dexClusterRoleName}

			By("waiting for initial ClusterRole creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crNN, &rbacv1.ClusterRole{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRole")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: crNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRole is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ClusterRoleBinding when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: dexClusterRoleBindingName}})

			crbNN := types.NamespacedName{Name: dexClusterRoleBindingName}

			By("waiting for initial ClusterRoleBinding creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, crbNN, &rbacv1.ClusterRoleBinding{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ClusterRoleBinding")
			Expect(k8sClient.Delete(ctx, &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: crbNN.Name},
			})).To(Succeed())

			By("verifying the ClusterRoleBinding is recreated with ownership labels")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Certificate when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			certNN := types.NamespacedName{Name: servingCertificateName, Namespace: uiNamespace}

			By("waiting for initial Certificate creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, certNN, &certmanagerv1.Certificate{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Certificate")
			Expect(k8sClient.Delete(ctx, &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{Name: certNN.Name, Namespace: certNN.Namespace},
			})).To(Succeed())

			By("verifying the Certificate is recreated with ownership labels")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates Issuer when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			issuerNN := types.NamespacedName{Name: caIssuerName, Namespace: uiNamespace}

			By("waiting for initial Issuer creation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, issuerNN, &certmanagerv1.Issuer{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the Issuer")
			Expect(k8sClient.Delete(ctx, &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{Name: issuerNN.Name, Namespace: issuerNN.Namespace},
			})).To(Succeed())

			By("verifying the Issuer is recreated with ownership labels")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("recreates ConfigMap when deleted", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			By("waiting for initial ConfigMap creation (discovered by label)")
			var cmName string
			Eventually(func(g Gomega) {
				cmList := &corev1.ConfigMapList{}
				g.Expect(k8sClient.List(ctx, cmList,
					client.InNamespace(uiNamespace),
					client.MatchingLabels{dexConfigMapLabel: "true"},
				)).To(Succeed())
				g.Expect(cmList.Items).NotTo(BeEmpty(), "expected at least one dex ConfigMap")
				cmName = cmList.Items[0].Name
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("deleting the ConfigMap")
			Expect(k8sClient.Delete(ctx, &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: uiNamespace},
			})).To(Succeed())

			By("verifying a ConfigMap with the managed label is recreated")
			Eventually(func(g Gomega) {
				cmList := &corev1.ConfigMapList{}
				g.Expect(k8sClient.List(ctx, cmList,
					client.InNamespace(uiNamespace),
					client.MatchingLabels{dexConfigMapLabel: "true"},
				)).To(Succeed())
				g.Expect(cmList.Items).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Drift correction", func() {
		It("restores Deployment image when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			deploymentNN := types.NamespacedName{Name: proxyDeploymentName, Namespace: uiNamespace}

			By("waiting for initial Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, reverseProxyContainerName)
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, reverseProxyContainerName)
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, reverseProxyContainerName)
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ServiceAccount labels when stripped", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			saNN := types.NamespacedName{Name: proxyServiceAccountName, Namespace: uiNamespace}

			By("waiting for initial ServiceAccount creation with ownership labels")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the ServiceAccount")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				delete(sa.Labels, constant.KonfluxOwnerLabel)
				delete(sa.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, sa)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ServiceAccount labels are restored")
			Eventually(func(g Gomega) {
				sa := &corev1.ServiceAccount{}
				g.Expect(k8sClient.Get(ctx, saNN, sa)).To(Succeed())
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(sa.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service labels when stripped", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			svcNN := types.NamespacedName{Name: proxyServiceName, Namespace: uiNamespace}

			By("waiting for initial Service creation with ownership labels")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Service")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				delete(svc.Labels, constant.KonfluxOwnerLabel)
				delete(svc.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service labels are restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(svc.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Service spec when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			svcNN := types.NamespacedName{Name: proxyServiceName, Namespace: uiNamespace}

			By("waiting for initial Service creation")
			var originalTargetPort string
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				originalTargetPort = svc.Spec.Ports[0].TargetPort.StrVal
				g.Expect(originalTargetPort).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Service target port")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				svc.Spec.Ports[0].TargetPort.StrVal = "tampered"
				g.Expect(k8sClient.Update(ctx, svc)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Service target port is restored")
			Eventually(func(g Gomega) {
				svc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, svcNN, svc)).To(Succeed())
				g.Expect(svc.Spec.Ports).NotTo(BeEmpty())
				g.Expect(svc.Spec.Ports[0].TargetPort.StrVal).To(Equal(originalTargetPort))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Namespace labels when stripped", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			nsNN := types.NamespacedName{Name: uiNamespace}

			By("waiting for initial Namespace creation with ownership labels")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Namespace")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				delete(ns.Labels, constant.KonfluxOwnerLabel)
				delete(ns.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, ns)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Namespace labels are restored")
			Eventually(func(g Gomega) {
				ns := &corev1.Namespace{}
				g.Expect(k8sClient.Get(ctx, nsNN, ns)).To(Succeed())
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(ns.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRole rules when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: dexClusterRoleName}})

			crNN := types.NamespacedName{Name: dexClusterRoleName}

			By("waiting for initial ClusterRole creation")
			var originalRules []rbacv1.PolicyRule
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).NotTo(BeEmpty())
				originalRules = cr.Rules
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRole rules")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				cr.Rules = []rbacv1.PolicyRule{{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"delete"},
				}}
				g.Expect(k8sClient.Update(ctx, cr)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRole rules are restored")
			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, crNN, cr)).To(Succeed())
				g.Expect(cr.Rules).To(Equal(originalRules))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ClusterRoleBinding subjects when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: dexClusterRoleBindingName}})

			crbNN := types.NamespacedName{Name: dexClusterRoleBindingName}

			By("waiting for initial ClusterRoleBinding creation")
			var originalSubjects []rbacv1.Subject
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).NotTo(BeEmpty())
				originalSubjects = crb.Subjects
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ClusterRoleBinding subjects")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				crb.Subjects = []rbacv1.Subject{{
					Kind:      "ServiceAccount",
					Name:      "tampered-sa",
					Namespace: "default",
				}}
				g.Expect(k8sClient.Update(ctx, crb)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ClusterRoleBinding subjects are restored")
			Eventually(func(g Gomega) {
				crb := &rbacv1.ClusterRoleBinding{}
				g.Expect(k8sClient.Get(ctx, crbNN, crb)).To(Succeed())
				g.Expect(crb.Subjects).To(Equal(originalSubjects))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Certificate labels when stripped", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			certNN := types.NamespacedName{Name: servingCertificateName, Namespace: uiNamespace}

			By("waiting for initial Certificate creation with ownership labels")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Certificate")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				delete(cert.Labels, constant.KonfluxOwnerLabel)
				delete(cert.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, cert)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Certificate labels are restored")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(cert.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Issuer labels when stripped", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			issuerNN := types.NamespacedName{Name: caIssuerName, Namespace: uiNamespace}

			By("waiting for initial Issuer creation with ownership labels")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("stripping ownership labels from the Issuer")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				delete(issuer.Labels, constant.KonfluxOwnerLabel)
				delete(issuer.Labels, constant.KonfluxComponentLabel)
				g.Expect(k8sClient.Update(ctx, issuer)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Issuer labels are restored")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxOwnerLabel))
				g.Expect(issuer.Labels).To(HaveKey(constant.KonfluxComponentLabel))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Certificate DNSNames when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			certNN := types.NamespacedName{Name: servingCertificateName, Namespace: uiNamespace}

			By("waiting for initial Certificate creation")
			var originalDNSNames []string
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Spec.DNSNames).NotTo(BeEmpty())
				originalDNSNames = cert.Spec.DNSNames
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Certificate DNSNames")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				cert.Spec.DNSNames = []string{"tampered.example.com"}
				g.Expect(k8sClient.Update(ctx, cert)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Certificate DNSNames are restored")
			Eventually(func(g Gomega) {
				cert := &certmanagerv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, certNN, cert)).To(Succeed())
				g.Expect(cert.Spec.DNSNames).To(Equal(originalDNSNames))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Issuer CA secret name when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			issuerNN := types.NamespacedName{Name: caIssuerName, Namespace: uiNamespace}

			By("waiting for initial Issuer creation")
			var originalSecretName string
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Spec.CA).NotTo(BeNil())
				g.Expect(issuer.Spec.CA.SecretName).NotTo(BeEmpty())
				originalSecretName = issuer.Spec.CA.SecretName
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Issuer CA secret name")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				issuer.Spec.CA.SecretName = "tampered-secret"
				g.Expect(k8sClient.Update(ctx, issuer)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Issuer CA secret name is restored")
			Eventually(func(g Gomega) {
				issuer := &certmanagerv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerNN, issuer)).To(Succeed())
				g.Expect(issuer.Spec.CA.SecretName).To(Equal(originalSecretName))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores dex Deployment image when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			deploymentNN := types.NamespacedName{Name: dexDeploymentName, Namespace: uiNamespace}

			By("waiting for initial dex Deployment creation")
			var originalImage string
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, dexContainerName)
				g.Expect(container).NotTo(BeNil())
				originalImage = container.Image
				g.Expect(originalImage).NotTo(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the dex Deployment image")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, dexContainerName)
				g.Expect(container).NotTo(BeNil())
				container.Image = "tampered-image:latest"
				g.Expect(k8sClient.Update(ctx, dep)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the dex Deployment image is restored")
			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, deploymentNN, dep)).To(Succeed())
				container := testutil.FindContainer(dep.Spec.Template.Spec.Containers, dexContainerName)
				g.Expect(container).NotTo(BeNil())
				g.Expect(container.Image).To(Equal(originalImage))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores Ingress spec when modified", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "drift-test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui,
				&networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
					Name: ingress.IngressName, Namespace: uiNamespace,
				}},
			)

			ingressNN := types.NamespacedName{Name: ingress.IngressName, Namespace: uiNamespace}

			By("waiting for initial Ingress creation")
			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, ingressNN, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules).NotTo(BeEmpty())
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("drift-test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the Ingress host")
			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, ingressNN, ing)).To(Succeed())
				ing.Spec.Rules[0].Host = "tampered.example.com"
				g.Expect(k8sClient.Update(ctx, ing)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the Ingress host is restored")
			Eventually(func(g Gomega) {
				ing := &networkingv1.Ingress{}
				g.Expect(k8sClient.Get(ctx, ingressNN, ing)).To(Succeed())
				g.Expect(ing.Spec.Rules).NotTo(BeEmpty())
				g.Expect(ing.Spec.Rules[0].Host).To(Equal("drift-test.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("restores ConsoleLink spec when modified", func(ctx context.Context) {
			openShiftClusterInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
					},
				},
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			})
			Expect(err).NotTo(HaveOccurred())

			startManager(noDefaultSegmentKey, openShiftClusterInfo)

			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "consolelink-drift.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			testutil.DeferCleanupParentAndChildren(k8sClient, ui,
				&consolev1.ConsoleLink{ObjectMeta: metav1.ObjectMeta{Name: consolelink.ConsoleLinkName}},
			)

			consoleLinkNN := types.NamespacedName{Name: consolelink.ConsoleLinkName}

			By("waiting for initial ConsoleLink creation")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, consoleLinkNN, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-drift.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("modifying the ConsoleLink href")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, consoleLinkNN, cl)).To(Succeed())
				cl.Spec.Href = "https://tampered.example.com"
				g.Expect(k8sClient.Update(ctx, cl)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("verifying the ConsoleLink href is restored")
			Eventually(func(g Gomega) {
				cl := &consolev1.ConsoleLink{}
				g.Expect(k8sClient.Get(ctx, consoleLinkNN, cl)).To(Succeed())
				g.Expect(cl.Spec.Href).To(Equal("https://consolelink-drift.example.com"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Segment Secret reconciliation via Reconcile", Serial, func() {
		// expectedSecretName computes the hashed secret name for a given write key and API URL.
		expectedSecretName := func(writeKey, apiURL string) string {
			return hashedsecret.Build(segmentSecretBaseName, uiNamespace, map[string]string{
				segmentKeyWriteKey: writeKey,
				segmentKeyAPIURL:   apiURL,
			}).Name
		}

		// listSegmentSecrets returns all Secrets in uiNamespace whose name starts with the segment base name.
		listSegmentSecrets := func(ctx context.Context) []corev1.Secret {
			secretList := &corev1.SecretList{}
			ExpectWithOffset(1, k8sClient.List(ctx, secretList, client.InNamespace(uiNamespace))).To(Succeed())
			var result []corev1.Secret
			for _, s := range secretList.Items {
				if len(s.Name) > len(segmentSecretBaseName) && s.Name[:len(segmentSecretBaseName)] == segmentSecretBaseName {
					result = append(result, s)
				}
			}
			return result
		}

		BeforeEach(func(ctx context.Context) {
			By("pre-cleaning any existing segment secrets and Bridge CR")
			for _, s := range listSegmentSecrets(ctx) {
				_ = k8sClient.Delete(ctx, &s) //nolint:gosec
			}
			_ = k8sClient.Delete(ctx, &konfluxv1alpha1.KonfluxSegmentBridge{ObjectMeta: metav1.ObjectMeta{
				Name: segmentbridge.CRName,
			}})
		})

		// createUI creates the KonfluxUI CR and registers DeferCleanup for it and leftover secrets.
		createUI := func(ctx context.Context) *konfluxv1alpha1.KonfluxUI {
			ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
				for _, s := range listSegmentSecrets(ctx) {
					_ = k8sClient.Delete(ctx, &s) //nolint:gosec
				}
			})
			return ui
		}

		// createBridgeCR creates a KonfluxSegmentBridge CR and registers DeferCleanup for it.
		createBridgeCR := func(ctx context.Context) *konfluxv1alpha1.KonfluxSegmentBridge {
			bridge := &konfluxv1alpha1.KonfluxSegmentBridge{ObjectMeta: metav1.ObjectMeta{Name: segmentbridge.CRName}}
			Expect(k8sClient.Create(ctx, bridge)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, bridge)
			return bridge
		}

		It("Should not create segment secret when KonfluxSegmentBridge CR does not exist", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			By("waiting for initial reconcile to complete")
			waitForReconcile(ctx)

			Expect(listSegmentSecrets(ctx)).To(BeEmpty())
		})

		It("Should not create segment secret when no write key is configured", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			By("waiting for initial reconcile with empty Bridge key")
			waitForReconcile(ctx)

			_ = bridge
			Expect(listSegmentSecrets(ctx)).To(BeEmpty())
		})

		It("Should create segment secret with CR write key and default API URL", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "test-write-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			name := expectedSecretName("test-write-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: name, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("test-write-key"))
				g.Expect(string(secret.Data[segmentKeyAPIURL])).To(Equal(konfluxv1alpha1.DefaultSegmentAPIURL))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create segment secret with CR write key and custom API URL", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "test-write-key"
			bridge.Spec.SegmentAPIURL = "https://console.redhat.com/connections/api/v1"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			name := expectedSecretName("test-write-key", "https://console.redhat.com/connections/api/v1")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: name, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("test-write-key"))
				g.Expect(string(secret.Data[segmentKeyAPIURL])).To(Equal("https://console.redhat.com/connections/api/v1"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should use build-time default key when CR key is empty", func(ctx context.Context) {
			createBridgeCR(ctx)
			startManager(staticSegmentKey("build-time-key"), nil)
			createUI(ctx)

			name := expectedSecretName("build-time-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: name, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("build-time-key"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should prefer CR key over build-time default", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "cr-override-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(staticSegmentKey("build-time-key"), nil)
			createUI(ctx)

			name := expectedSecretName("cr-override-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: name, Namespace: uiNamespace,
				}, secret)).To(Succeed())
				g.Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("cr-override-key"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should create a new secret and clean up old one when segment key changes", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "initial-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			initialName := expectedSecretName("initial-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: initialName, Namespace: uiNamespace,
				}, &corev1.Secret{})).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("changing the segment key")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "updated-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			updatedName := expectedSecretName("updated-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Expect(updatedName).NotTo(Equal(initialName), "hash should differ for different keys")

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: updatedName, Namespace: uiNamespace,
				}, &corev1.Secret{})).To(Succeed())
				g.Expect(errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: initialName, Namespace: uiNamespace,
				}, &corev1.Secret{}))).To(BeTrue(), "old segment secret should be deleted")
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should clean up segment secret when key becomes empty", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "temporary-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			By("waiting for the secret to be created")
			Eventually(func(g Gomega) {
				g.Expect(listSegmentSecrets(ctx)).To(HaveLen(1))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			By("removing the segment key")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = ""
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(listSegmentSecrets(ctx)).To(BeEmpty())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should set owner reference on created segment secret", func(ctx context.Context) {
			bridge := createBridgeCR(ctx)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, bridge)).To(Succeed())
			bridge.Spec.SegmentKey = "owner-ref-test-key"
			Expect(k8sClient.Update(ctx, bridge)).To(Succeed())

			startManager(noDefaultSegmentKey, nil)
			createUI(ctx)

			Eventually(func(g Gomega) {
				secrets := listSegmentSecrets(ctx)
				g.Expect(secrets).To(HaveLen(1))
				g.Expect(secrets[0].OwnerReferences).To(HaveLen(1))
				g.Expect(secrets[0].OwnerReferences[0].Name).To(Equal(CRName))
				g.Expect(secrets[0].OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("RuntimeConfig reconciliation via Reconcile", Serial, func() {
		BeforeEach(func() {
			startManager(noDefaultSegmentKey, nil)
		})

		It("Should set RUNTIME_* env vars on generate-proxy-config init container", func(ctx context.Context) {
			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					RuntimeConfig: &konfluxv1alpha1.RuntimeConfigSpec{
						ChatBot: &konfluxv1alpha1.ChatBotConfig{
							Enabled: ptr.To(false),
						},
						Monitoring: &konfluxv1alpha1.MonitoringConfig{
							Enabled:     ptr.To(true),
							DSN:         "https://example@sentry.io/123",
							Environment: "staging",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: proxyDeploymentName, Namespace: uiNamespace,
				}, dep)).To(Succeed())

				var initContainer *corev1.Container
				for i := range dep.Spec.Template.Spec.InitContainers {
					if dep.Spec.Template.Spec.InitContainers[i].Name == generateProxyConfigContainerName {
						initContainer = &dep.Spec.Template.Spec.InitContainers[i]
						break
					}
				}
				g.Expect(initContainer).NotTo(BeNil())

				envMap := make(map[string]string, len(initContainer.Env))
				for _, e := range initContainer.Env {
					envMap[e.Name] = e.Value
				}
				g.Expect(envMap).To(HaveKeyWithValue("RUNTIME_CHAT_BOT_ENABLED", "false"))
				g.Expect(envMap).To(HaveKeyWithValue("RUNTIME_MONITORING_ENABLED", "true"))
				g.Expect(envMap).To(HaveKeyWithValue("RUNTIME_MONITORING_DSN", "https://example@sentry.io/123"))
				g.Expect(envMap).To(HaveKeyWithValue("RUNTIME_MONITORING_ENVIRONMENT", "staging"))
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("Should not set RUNTIME_* env vars when runtimeConfig is nil", func(ctx context.Context) {
			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec:       konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(testutil.DeleteAndWait, k8sClient, ui)

			Eventually(func(g Gomega) {
				dep := &appsv1.Deployment{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name: proxyDeploymentName, Namespace: uiNamespace,
				}, dep)).To(Succeed())

				var initContainer *corev1.Container
				for i := range dep.Spec.Template.Spec.InitContainers {
					if dep.Spec.Template.Spec.InitContainers[i].Name == generateProxyConfigContainerName {
						initContainer = &dep.Spec.Template.Spec.InitContainers[i]
						break
					}
				}
				g.Expect(initContainer).NotTo(BeNil())

				for _, e := range initContainer.Env {
					g.Expect(e.Name).NotTo(HavePrefix("RUNTIME_"))
				}
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})
	})

	Context("Component metrics gating via Reconcile", Serial, func() {
		serviceMonitorGVK := schema.GroupVersionKind{
			Group:   "monitoring.coreos.com",
			Version: "v1",
			Kind:    "ServiceMonitor",
		}
		serviceMonitorNN := types.NamespacedName{
			Name:      "konflux-ui-proxy",
			Namespace: uiNamespace,
		}
		metricsReaderCRName := "konflux-ui-proxy-metrics-reader"

		getServiceMonitor := func(ctx context.Context) (*unstructured.Unstructured, error) {
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			err := k8sClient.Get(ctx, serviceMonitorNN, sm)
			return sm, err
		}

		cleanupMetricsResources := func(ctx context.Context) {
			sm := &unstructured.Unstructured{}
			sm.SetGroupVersionKind(serviceMonitorGVK)
			sm.SetName(serviceMonitorNN.Name)
			sm.SetNamespace(serviceMonitorNN.Namespace)
			_ = client.IgnoreNotFound(k8sClient.Delete(ctx, sm))

			_ = client.IgnoreNotFound(k8sClient.Delete(ctx, &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Name: metricsReaderCRName},
			}))
		}

		It("applies ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics is nil (default enabled)", func(ctx context.Context) {
			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec:       konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
				cleanupMetricsResources(ctx)
			})

			Eventually(func(g Gomega) {
				_, err := getServiceMonitor(ctx)
				g.Expect(err).NotTo(HaveOccurred())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

			Eventually(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: metricsReaderCRName}, cr)).To(Succeed())
			}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
		})

		It("skips ServiceMonitor and metrics-reader ClusterRole when ComponentMetrics.Enabled is false", func(ctx context.Context) {
			cleanupMetricsResources(ctx)

			startManager(noDefaultSegmentKey, nil)

			ui := &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{
						Enabled: ptr.To(false),
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, ui)
			})

			waitForReconcile(ctx)

			Consistently(func(g Gomega) {
				_, err := getServiceMonitor(ctx)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())

			Consistently(func(g Gomega) {
				cr := &rbacv1.ClusterRole{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: metricsReaderCRName}, cr)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}).WithTimeout(3 * time.Second).WithPolling(500 * time.Millisecond).Should(Succeed())
		})
	})
})

// mockDiscoveryClient implements clusterinfo.DiscoveryClient for testing.
type mockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}
