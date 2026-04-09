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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	consolev1 "github.com/openshift/api/console/v1"

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

func noDefaultSegmentKey() string             { return "" }
func staticSegmentKey(k string) func() string { return func() string { return k } }

var _ = Describe("KonfluxUI Controller", func() {
	Context("When reconciling a resource", Ordered, func() {
		BeforeAll(func(ctx context.Context) {
			mgr := testutil.NewTestManager(testEnv)
			Expect((&KonfluxUIReconciler{
				Client:               mgr.GetClient(),
				Scheme:               mgr.GetScheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}).SetupWithManager(mgr)).To(Succeed())
			mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
			DeferCleanup(cancel)
			testutil.StartManagerWithContext(mgrCtx, mgr)

			Expect(k8sClient.Create(ctx, &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Name: CRName},
			})).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				testutil.DeleteAndWait(ctx, k8sClient, &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{Name: CRName},
				})
			})

		// We wait for all Deployments rather than a Ready status condition because
		// envtest does not run a kubelet, so replica status is never updated.
		By("waiting for all Deployments to exist as proof of successful reconciliation")
		deploymentNames := testutil.ResourceNamesFromManifest(manifests.UI, "Deployment")
		Eventually(func(g Gomega) {
			for _, name := range deploymentNames {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      name,
					Namespace: uiNamespace,
				}, &appsv1.Deployment{})).To(Succeed())
			}
		}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(Succeed())
	})

		It("should create the Namespace", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "Namespace") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &corev1.Namespace{})).
					To(Succeed(), "expected Namespace %q to exist", name)
			}
		})

		It("should create the Services", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "Service") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: uiNamespace}, &corev1.Service{})).
					To(Succeed(), "expected Service %q to exist in namespace %q", name, uiNamespace)
			}
		})

		It("should create the ConfigMaps", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "ConfigMap") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: uiNamespace}, &corev1.ConfigMap{})).
					To(Succeed(), "expected ConfigMap %q to exist in namespace %q", name, uiNamespace)
			}
		})

		It("should create the Secrets", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "Secret") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: uiNamespace}, &corev1.Secret{})).
					To(Succeed(), "expected Secret %q to exist in namespace %q", name, uiNamespace)
			}
		})

		It("should create the ClusterRoles", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "ClusterRole") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &rbacv1.ClusterRole{})).
					To(Succeed(), "expected ClusterRole %q to exist", name)
			}
		})

		It("should create the ClusterRoleBindings", func(ctx context.Context) {
			for _, name := range testutil.ResourceNamesFromManifest(manifests.UI, "ClusterRoleBinding") {
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, &rbacv1.ClusterRoleBinding{})).
					To(Succeed(), "expected ClusterRoleBinding %q to exist", name)
			}
		})
	})

	Context("ensureUISecrets", func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		BeforeEach(func(ctx context.Context) {
			By("cleaning up any existing secrets from previous tests")
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, clientSecret)

			cookieSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-cookie-secret",
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, cookieSecret)

			By("creating the KonfluxUI resource")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}

			By("creating the UI namespace")
			uiNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: uiNamespace,
				},
			}
			err := k8sClient.Create(ctx, uiNs)
			if err != nil && !errors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up secrets")
			clientSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
				},
			}
			err := k8sClient.Delete(ctx, clientSecret)
			if err != nil && !errors.IsNotFound(err) {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to delete client secret: %v\n", err)
			}

			cookieSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-cookie-secret",
					Namespace: uiNamespace,
				},
			}
			err = k8sClient.Delete(ctx, cookieSecret)
			if err != nil && !errors.IsNotFound(err) {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to delete cookie secret: %v\n", err)
			}

			By("cleaning up KonfluxUI resource")
			err = k8sClient.Delete(ctx, ui)
			if err != nil && !errors.IsNotFound(err) {
				_, _ = fmt.Fprintf(GinkgoWriter, "Failed to delete KonfluxUI: %v\n", err)
			}
		})

		It("Should preserve existing secret data when secret already exists with valid data", func(ctx context.Context) {
			By("creating a secret with existing data")
			existingData := []byte("existing-secret-value")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
					Labels: map[string]string{
						constant.KonfluxOwnerLabel:     CRName,
						constant.KonfluxComponentLabel: string(manifests.UI),
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "konflux.konflux-ci.dev/v1alpha1",
							Kind:               "KonfluxUI",
							Name:               ui.Name,
							UID:                ui.UID,
							Controller:         func() *bool { b := true; return &b }(),
							BlockOwnerDeletion: func() *bool { b := true; return &b }(),
						},
					},
				},
				Data: map[string][]byte{
					"client-secret": existingData,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying the secret data was preserved")
			updatedSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Data["client-secret"]).To(Equal(existingData))
		})

		It("Should update ownership labels and owner reference when secret exists but has incorrect ownership", func(ctx context.Context) {
			By("creating a secret with incorrect ownership labels and no owner reference")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
					Labels: map[string]string{
						constant.KonfluxOwnerLabel:     "wrong-owner",
						constant.KonfluxComponentLabel: "wrong-component",
					},
				},
				Data: map[string][]byte{
					"client-secret": []byte("existing-value"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying the ownership labels were updated")
			updatedSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Labels).To(HaveKeyWithValue(constant.KonfluxOwnerLabel, CRName))
			Expect(updatedSecret.Labels).To(HaveKeyWithValue(constant.KonfluxComponentLabel, string(manifests.UI)))

			By("verifying the owner reference was added")
			Expect(updatedSecret.OwnerReferences).To(HaveLen(1))
			Expect(updatedSecret.OwnerReferences[0].Name).To(Equal(CRName))
		})

		It("Should regenerate secret data when secret exists but has empty data", func(ctx context.Context) {
			By("creating a secret with empty data")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
				},
				Data: map[string][]byte{},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying the secret data was generated")
			updatedSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)).To(Succeed())
			Expect(updatedSecret.Data).To(HaveKey("client-secret"))
			Expect(updatedSecret.Data["client-secret"]).ToNot(BeEmpty())
		})

		It("Should use URL-safe base64 encoding for client-secret", func(ctx context.Context) {
			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying the client-secret uses URL-safe encoding")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			clientSecretValue := string(secret.Data["client-secret"])
			By("verifying no padding characters (URL-safe uses RawURLEncoding)")
			Expect(clientSecretValue).NotTo(ContainSubstring("="))
			By("verifying it contains only URL-safe characters")
			Expect(clientSecretValue).To(MatchRegexp("^[A-Za-z0-9_-]+$"))
		})

		It("Should use standard base64 encoding for cookie-secret", func(ctx context.Context) {
			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying the cookie-secret uses standard encoding")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-cookie-secret",
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			cookieSecretValue := string(secret.Data["cookie-secret"])
			By("verifying it contains standard base64 characters (may include + and /)")
			Expect(cookieSecretValue).To(MatchRegexp("^[A-Za-z0-9+/]+=*$"))
		})

		It("Should create both secrets in a single call", func(ctx context.Context) {
			By("calling Reconcile")
			reconcileUI(ctx)

			By("verifying both secrets were created")
			clientSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, clientSecret)).To(Succeed())

			cookieSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-cookie-secret",
				Namespace: uiNamespace,
			}, cookieSecret)).To(Succeed())
		})

		It("Should be idempotent when called multiple times", func(ctx context.Context) {
			By("calling Reconcile first time")
			reconcileUI(ctx)

			By("getting the first secret value")
			secret1 := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, secret1)
			Expect(err).NotTo(HaveOccurred())
			firstValue := secret1.Data["client-secret"]

			By("calling Reconcile second time")
			reconcileUI(ctx)

			By("verifying the secret value was not changed")
			secret2 := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, secret2)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret2.Data["client-secret"]).To(Equal(firstValue))
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
		var reconciler *KonfluxUIReconciler

		// Helper: refresh UI from cluster
		refreshUI := func(ctx context.Context) {
			ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: ui.Name}, ui)).To(Succeed())
		}

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		// Helper: get Ingress resource
		getIngress := func(ctx context.Context) *networkingv1.Ingress {
			ing := &networkingv1.Ingress{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      ingress.IngressName,
				Namespace: uiNamespace,
			}, ing)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return ing
		}

		// Helper: check if Ingress exists
		ingressExists := func(ctx context.Context) bool {
			ing := &networkingv1.Ingress{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      ingress.IngressName,
				Namespace: uiNamespace,
			}, ing)
			return err == nil
		}

		// Helper: enable ingress with host and reconcile
		enableIngressAndReconcile := func(ctx context.Context, host string) {
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				Enabled: ptr.To(true),
				Host:    host,
			}
			ExpectWithOffset(1, k8sClient.Update(ctx, ui)).To(Succeed())
			reconcileUI(ctx)
		}

		BeforeEach(func(ctx context.Context) {
			By("cleaning up any existing ingress from previous tests")
			existingIngress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingress.IngressName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingIngress)

			By("creating the KonfluxUI resource")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up ingress")
			existingIngress := &networkingv1.Ingress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ingress.IngressName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingIngress)

			By("cleaning up KonfluxUI resource")
			_ = k8sClient.Delete(ctx, ui)
		})

		It("Should create Ingress when ingress is enabled", func(ctx context.Context) {
			enableIngressAndReconcile(ctx, "test.example.com")

			By("verifying the Ingress was created")
			ing := getIngress(ctx)
			Expect(ing.Spec.Rules).To(HaveLen(1))
			Expect(ing.Spec.Rules[0].Host).To(Equal("test.example.com"))
		})

		It("Should delete Ingress when ingress spec is set to nil", func(ctx context.Context) {
			By("first enabling ingress and reconciling to create the Ingress")
			enableIngressAndReconcile(ctx, "test.example.com")
			Expect(ingressExists(ctx)).To(BeTrue())

			By("disabling ingress by setting spec to nil")
			refreshUI(ctx)
			ui.Spec.Ingress = nil
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())
			reconcileUI(ctx)

			By("verifying the Ingress was deleted")
			Expect(ingressExists(ctx)).To(BeFalse())
		})

		It("Should delete Ingress when Enabled is set to false", func(ctx context.Context) {
			By("first enabling ingress and reconciling to create the Ingress")
			enableIngressAndReconcile(ctx, "test.example.com")
			Expect(ingressExists(ctx)).To(BeTrue())

			By("setting Enabled to false in the KonfluxUI spec")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())
			reconcileUI(ctx)

			By("verifying the Ingress was deleted")
			Expect(ingressExists(ctx)).To(BeFalse())
		})

		It("Should not error when ingress is disabled and no Ingress exists", func(ctx context.Context) {
			By("ensuring no Ingress exists")
			Expect(ingressExists(ctx)).To(BeFalse())

			By("reconciling with ingress disabled - should not error")
			reconcileUI(ctx)
		})

		It("Should update Ingress when hostname changes", func(ctx context.Context) {
			By("enabling ingress with initial hostname")
			enableIngressAndReconcile(ctx, "initial.example.com")
			Expect(getIngress(ctx).Spec.Rules[0].Host).To(Equal("initial.example.com"))

			By("updating to new hostname")
			refreshUI(ctx)
			ui.Spec.Ingress.Host = "updated.example.com"
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())
			reconcileUI(ctx)

			By("verifying the Ingress was updated with new hostname")
			Expect(getIngress(ctx).Spec.Rules[0].Host).To(Equal("updated.example.com"))
		})

		It("Should include OpenShift TLS annotations on created Ingress", func(ctx context.Context) {
			enableIngressAndReconcile(ctx, "test.example.com")

			By("verifying the Ingress has OpenShift TLS annotations")
			ing := getIngress(ctx)
			Expect(ing.Annotations).To(HaveKeyWithValue(
				"route.openshift.io/destination-ca-certificate-secret", "ui-ca"))
			Expect(ing.Annotations).To(HaveKeyWithValue(
				"route.openshift.io/termination", "reencrypt"))
		})

		It("Should set owner reference on created Ingress", func(ctx context.Context) {
			enableIngressAndReconcile(ctx, "test.example.com")

			By("verifying the Ingress has owner reference")
			ing := getIngress(ctx)
			Expect(ing.OwnerReferences).To(HaveLen(1))
			Expect(ing.OwnerReferences[0].Name).To(Equal(CRName))
			Expect(ing.OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
		})

		It("Should update KonfluxUI status with ingress information", func(ctx context.Context) {
			enableIngressAndReconcile(ctx, "status-test.example.com")

			By("verifying the KonfluxUI status has ingress information")
			updatedUI := &konfluxv1alpha1.KonfluxUI{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ui.Name}, updatedUI)).To(Succeed())
			Expect(updatedUI.Status.Ingress).NotTo(BeNil())
			Expect(updatedUI.Status.Ingress.Enabled).To(BeTrue())
			Expect(updatedUI.Status.Ingress.Hostname).To(Equal("status-test.example.com"))
			Expect(updatedUI.Status.Ingress.URL).To(Equal("https://status-test.example.com"))
		})
	})

	Context("OpenShift OAuth reconciliation via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler
		var openShiftClusterInfo *clusterinfo.Info
		var defaultClusterInfo *clusterinfo.Info

		// Helper: refresh UI from cluster
		refreshUI := func(ctx context.Context) {
			ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: ui.Name}, ui)).To(Succeed())
		}

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		// Helper: check if OpenShift OAuth ServiceAccount exists
		serviceAccountExists := func(ctx context.Context) bool {
			sa := &corev1.ServiceAccount{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      dex.DexClientServiceAccountName,
				Namespace: uiNamespace,
			}, sa)
			return err == nil
		}

		// Helper: check if OpenShift OAuth Secret exists
		secretExists := func(ctx context.Context) bool {
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      dex.DexClientSecretName,
				Namespace: uiNamespace,
			}, secret)
			return err == nil
		}

		// Helper: get OpenShift OAuth ServiceAccount
		getServiceAccount := func(ctx context.Context) *corev1.ServiceAccount {
			sa := &corev1.ServiceAccount{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      dex.DexClientServiceAccountName,
				Namespace: uiNamespace,
			}, sa)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return sa
		}

		// Helper: get OpenShift OAuth Secret
		getSecret := func(ctx context.Context) *corev1.Secret {
			secret := &corev1.Secret{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      dex.DexClientSecretName,
				Namespace: uiNamespace,
			}, secret)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return secret
		}

		BeforeEach(func(ctx context.Context) {
			By("cleaning up any existing OpenShift OAuth resources from previous tests")
			existingSA := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dex.DexClientServiceAccountName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingSA)

			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dex.DexClientSecretName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingSecret)

			By("creating mock cluster info for OpenShift and non-OpenShift platforms")
			var err error
			openShiftClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{
							{Kind: "ClusterVersion"},
						},
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

			By("creating the KonfluxUI resource with ingress enabled")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "openshift-test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				ClusterInfo:          nil, // Will be set in individual tests
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up OpenShift OAuth ServiceAccount")
			existingSA := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dex.DexClientServiceAccountName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingSA)

			By("cleaning up OpenShift OAuth Secret")
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dex.DexClientSecretName,
					Namespace: uiNamespace,
				},
			}
			_ = k8sClient.Delete(ctx, existingSecret)

			By("cleaning up KonfluxUI resource")
			_ = k8sClient.Delete(ctx, ui)
		})

		It("Should create OpenShift OAuth resources when running on OpenShift (default behavior)", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was created")
			Expect(serviceAccountExists(ctx)).To(BeTrue())
			sa := getServiceAccount(ctx)
			Expect(sa.Annotations).To(HaveKeyWithValue(
				"serviceaccounts.openshift.io/oauth-redirecturi.dex",
				"https://openshift-test.example.com/idp/callback",
			))

			By("verifying the Secret was created")
			Expect(secretExists(ctx)).To(BeTrue())
			secret := getSecret(ctx)
			Expect(secret.Type).To(Equal(corev1.SecretTypeServiceAccountToken))
			Expect(secret.Annotations).To(HaveKeyWithValue(
				"kubernetes.io/service-account.name",
				dex.DexClientServiceAccountName,
			))
		})

		It("Should NOT create OpenShift OAuth resources when NOT running on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to non-OpenShift")
			reconciler.ClusterInfo = defaultClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was NOT created")
			Expect(serviceAccountExists(ctx)).To(BeFalse())

			By("verifying the Secret was NOT created")
			Expect(secretExists(ctx)).To(BeFalse())
		})

		It("Should NOT create OpenShift OAuth resources when ClusterInfo is nil", func(ctx context.Context) {
			By("keeping ClusterInfo as nil")
			reconciler.ClusterInfo = nil

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was NOT created")
			Expect(serviceAccountExists(ctx)).To(BeFalse())

			By("verifying the Secret was NOT created")
			Expect(secretExists(ctx)).To(BeFalse())
		})

		It("Should NOT create OpenShift OAuth resources when explicitly disabled on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("explicitly disabling OpenShift login")
			refreshUI(ctx)
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(false),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was NOT created")
			Expect(serviceAccountExists(ctx)).To(BeFalse())

			By("verifying the Secret was NOT created")
			Expect(secretExists(ctx)).To(BeFalse())
		})

		It("Should create OpenShift OAuth resources when explicitly enabled on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("explicitly enabling OpenShift login")
			refreshUI(ctx)
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(true),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was created")
			Expect(serviceAccountExists(ctx)).To(BeTrue())

			By("verifying the Secret was created")
			Expect(secretExists(ctx)).To(BeTrue())
		})

		It("Should delete OpenShift OAuth resources when disabled after being enabled", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("first reconciling to create the resources")
			reconcileUI(ctx)
			Expect(serviceAccountExists(ctx)).To(BeTrue())
			Expect(secretExists(ctx)).To(BeTrue())

			By("disabling OpenShift login")
			refreshUI(ctx)
			ui.Spec.Dex = &konfluxv1alpha1.DexDeploymentSpec{
				Config: &dex.DexParams{
					ConfigureLoginWithOpenShift: ptr.To(false),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the ServiceAccount was deleted")
			Expect(serviceAccountExists(ctx)).To(BeFalse())

			By("verifying the Secret was deleted")
			Expect(secretExists(ctx)).To(BeFalse())
		})

		It("Should set owner reference on OpenShift OAuth resources", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ServiceAccount has owner reference")
			sa := getServiceAccount(ctx)
			Expect(sa.OwnerReferences).To(HaveLen(1))
			Expect(sa.OwnerReferences[0].Name).To(Equal(CRName))
			Expect(sa.OwnerReferences[0].Kind).To(Equal("KonfluxUI"))

			By("verifying the Secret has owner reference")
			secret := getSecret(ctx)
			Expect(secret.OwnerReferences).To(HaveLen(1))
			Expect(secret.OwnerReferences[0].Name).To(Equal(CRName))
			Expect(secret.OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
		})

		It("Should use correct redirect URI format without port", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the redirect URI has correct format")
			sa := getServiceAccount(ctx)
			// The redirect URI should be the hostname followed by the Dex callback path
			Expect(sa.Annotations["serviceaccounts.openshift.io/oauth-redirecturi.dex"]).To(
				Equal("https://openshift-test.example.com/idp/callback"),
			)
		})
	})

	Context("NodePort Service configuration via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler

		// Helper: refresh UI from cluster
		refreshUI := func(ctx context.Context) {
			ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: ui.Name}, ui)).To(Succeed())
		}

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		// Helper: get proxy Service
		getProxyService := func(ctx context.Context) *corev1.Service {
			svc := &corev1.Service{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      proxyServiceName,
				Namespace: uiNamespace,
			}, svc)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return svc
		}

		BeforeEach(func(ctx context.Context) {
			By("creating the KonfluxUI resource")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up KonfluxUI resource")
			_ = k8sClient.Delete(ctx, ui)
		})

		It("Should create proxy Service as ClusterIP by default", func(ctx context.Context) {
			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the proxy Service is ClusterIP")
			svc := getProxyService(ctx)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})

		It("Should create proxy Service as NodePort when nodePortService is configured", func(ctx context.Context) {
			By("configuring nodePortService")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the proxy Service is NodePort")
			svc := getProxyService(ctx)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		})

		It("Should set specific HTTPS NodePort when httpsPort is specified", func(ctx context.Context) {
			By("configuring nodePortService with specific httpsPort")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{
					HTTPSPort: ptr.To(int32(30443)),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the proxy Service has the specified NodePort")
			svc := getProxyService(ctx)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))

			var httpsPort *corev1.ServicePort
			for i := range svc.Spec.Ports {
				if svc.Spec.Ports[i].Name == "web-tls" {
					httpsPort = &svc.Spec.Ports[i]
					break
				}
			}
			Expect(httpsPort).NotTo(BeNil())
			Expect(httpsPort.NodePort).To(Equal(int32(30443)))
		})

		It("Should change proxy Service from ClusterIP to NodePort when nodePortService is added", func(ctx context.Context) {
			By("reconciling without nodePortService")
			reconcileUI(ctx)
			Expect(getProxyService(ctx).Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

			By("adding nodePortService configuration")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{
					HTTPSPort: ptr.To(int32(30444)),
				},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the proxy Service changed to NodePort")
			svc := getProxyService(ctx)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeNodePort))
		})

		It("Should change proxy Service from NodePort to ClusterIP when nodePortService is removed", func(ctx context.Context) {
			By("configuring nodePortService and reconciling")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{
				NodePortService: &konfluxv1alpha1.NodePortServiceSpec{},
			}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())
			reconcileUI(ctx)
			Expect(getProxyService(ctx).Spec.Type).To(Equal(corev1.ServiceTypeNodePort))

			By("removing nodePortService configuration")
			refreshUI(ctx)
			ui.Spec.Ingress = nil
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the proxy Service changed back to ClusterIP")
			svc := getProxyService(ctx)
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
		})
	})

	Context("ConsoleLink reconciliation via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler
		var openShiftClusterInfo *clusterinfo.Info
		var defaultClusterInfo *clusterinfo.Info

		// Helper: refresh UI from cluster
		refreshUI := func(ctx context.Context) {
			ExpectWithOffset(1, k8sClient.Get(ctx, types.NamespacedName{Name: ui.Name}, ui)).To(Succeed())
		}

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		// Helper: check if ConsoleLink exists
		consoleLinkExists := func(ctx context.Context) bool {
			cl := &consolev1.ConsoleLink{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: consolelink.ConsoleLinkName,
			}, cl)
			return err == nil
		}

		// Helper: get ConsoleLink resource
		getConsoleLink := func(ctx context.Context) *consolev1.ConsoleLink {
			cl := &consolev1.ConsoleLink{}
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name: consolelink.ConsoleLinkName,
			}, cl)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			return cl
		}

		BeforeEach(func(ctx context.Context) {
			By("cleaning up any existing ConsoleLink from previous tests")
			existingCL := &consolev1.ConsoleLink{
				ObjectMeta: metav1.ObjectMeta{
					Name: consolelink.ConsoleLinkName,
				},
			}
			_ = k8sClient.Delete(ctx, existingCL)

			By("creating mock cluster info for OpenShift and non-OpenShift platforms")
			var err error
			openShiftClusterInfo, err = clusterinfo.DetectWithClient(&mockDiscoveryClient{
				resources: map[string]*metav1.APIResourceList{
					"config.openshift.io/v1": {
						APIResources: []metav1.APIResource{
							{Kind: "ClusterVersion"},
						},
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

			By("creating the KonfluxUI resource with ingress enabled")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{
					Ingress: &konfluxv1alpha1.IngressSpec{
						Enabled: ptr.To(true),
						Host:    "consolelink-test.example.com",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				ClusterInfo:          nil, // Will be set in individual tests
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up ConsoleLink")
			existingCL := &consolev1.ConsoleLink{
				ObjectMeta: metav1.ObjectMeta{
					Name: consolelink.ConsoleLinkName,
				},
			}
			_ = k8sClient.Delete(ctx, existingCL)

			By("cleaning up KonfluxUI resource")
			_ = k8sClient.Delete(ctx, ui)
		})

		It("Should create ConsoleLink when ingress is enabled and running on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ConsoleLink was created")
			Expect(consoleLinkExists(ctx)).To(BeTrue())

			By("verifying the ConsoleLink has correct configuration")
			cl := getConsoleLink(ctx)
			Expect(cl.Spec.Href).To(Equal("https://consolelink-test.example.com"))
			Expect(cl.Spec.Text).To(Equal("Konflux Console"))
			Expect(cl.Spec.Location).To(Equal(consolev1.ApplicationMenu))
			Expect(cl.Spec.ApplicationMenu).NotTo(BeNil())
			Expect(cl.Spec.ApplicationMenu.Section).To(Equal("Konflux"))
		})

		It("Should NOT create ConsoleLink when NOT running on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to non-OpenShift")
			reconciler.ClusterInfo = defaultClusterInfo

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ConsoleLink was NOT created")
			Expect(consoleLinkExists(ctx)).To(BeFalse())
		})

		It("Should NOT create ConsoleLink when ClusterInfo is nil", func(ctx context.Context) {
			By("keeping ClusterInfo as nil")
			reconciler.ClusterInfo = nil

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ConsoleLink was NOT created")
			Expect(consoleLinkExists(ctx)).To(BeFalse())
		})

		It("Should NOT create ConsoleLink when ingress is disabled on OpenShift", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("disabling ingress")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the ConsoleLink was NOT created")
			Expect(consoleLinkExists(ctx)).To(BeFalse())
		})

		It("Should delete ConsoleLink when ingress is disabled", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("first reconciling to create the ConsoleLink")
			reconcileUI(ctx)
			Expect(consoleLinkExists(ctx)).To(BeTrue())

			By("disabling ingress")
			refreshUI(ctx)
			ui.Spec.Ingress = &konfluxv1alpha1.IngressSpec{Enabled: ptr.To(false)}
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the ConsoleLink was deleted")
			Expect(consoleLinkExists(ctx)).To(BeFalse())
		})

		It("Should update ConsoleLink when hostname changes", func(ctx context.Context) {
			By("setting ClusterInfo to OpenShift")
			reconciler.ClusterInfo = openShiftClusterInfo

			By("reconciling to create the ConsoleLink")
			reconcileUI(ctx)
			Expect(getConsoleLink(ctx).Spec.Href).To(Equal("https://consolelink-test.example.com"))

			By("updating the hostname")
			refreshUI(ctx)
			ui.Spec.Ingress.Host = "updated-consolelink.example.com"
			Expect(k8sClient.Update(ctx, ui)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the ConsoleLink href was updated")
			Expect(getConsoleLink(ctx).Spec.Href).To(Equal("https://updated-consolelink.example.com"))
		})
	})

	Context("Segment Secret reconciliation via Reconcile", Serial, func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler
		var segmentBridgeCR *konfluxv1alpha1.KonfluxSegmentBridge

		// Helper: reconcile and expect success
		reconcileUI := func(ctx context.Context) {
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: ui.Name},
			})
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
		}

		// Helper: build the expected hashed secret name for given data
		expectedSecretName := func(writeKey, apiURL string) string {
			s := hashedsecret.Build(segmentSecretBaseName, uiNamespace, map[string]string{
				segmentKeyWriteKey: writeKey,
				segmentKeyAPIURL:   apiURL,
			})
			return s.Name
		}

		// Helper: list all Secrets in the UI namespace matching the segment base name prefix
		listSegmentSecrets := func(ctx context.Context) []corev1.Secret {
			secretList := &corev1.SecretList{}
			err := k8sClient.List(ctx, secretList, client.InNamespace(uiNamespace))
			ExpectWithOffset(1, err).NotTo(HaveOccurred())
			var result []corev1.Secret
			for _, s := range secretList.Items {
				if len(s.Name) > len(segmentSecretBaseName) && s.Name[:len(segmentSecretBaseName)] == segmentSecretBaseName {
					result = append(result, s)
				}
			}
			return result
		}

		BeforeEach(func(ctx context.Context) {
			By("cleaning up any existing segment secrets from previous tests")
			for _, s := range listSegmentSecrets(ctx) {
				_ = k8sClient.Delete(ctx, &s)
			}

			By("cleaning up any existing KonfluxSegmentBridge CR")
			existingBridge := &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			_ = k8sClient.Delete(ctx, existingBridge)

			By("creating the KonfluxUI resource")
			ui = &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
				Spec: konfluxv1alpha1.KonfluxUISpec{},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			segmentBridgeCR = nil

			reconciler = &KonfluxUIReconciler{
				Client:               k8sClient,
				Scheme:               k8sClient.Scheme(),
				ObjectStore:          objectStore,
				GetDefaultSegmentKey: noDefaultSegmentKey,
			}
		})

		AfterEach(func(ctx context.Context) {
			By("cleaning up segment secrets")
			for _, s := range listSegmentSecrets(ctx) {
				_ = k8sClient.Delete(ctx, &s)
			}

			By("cleaning up KonfluxSegmentBridge CR")
			if segmentBridgeCR != nil {
				_ = k8sClient.Delete(ctx, segmentBridgeCR)
			}

			By("cleaning up KonfluxUI resource")
			_ = k8sClient.Delete(ctx, ui)
		})

		It("Should not create segment secret when KonfluxSegmentBridge CR does not exist", func(ctx context.Context) {
			By("reconciling without a KonfluxSegmentBridge CR")
			reconcileUI(ctx)

			By("verifying no segment secret was created")
			Expect(listSegmentSecrets(ctx)).To(BeEmpty())
		})

		It("Should not create segment secret when no write key is configured", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR without a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling with no default key either")
			reconcileUI(ctx)

			By("verifying no segment secret was created")
			Expect(listSegmentSecrets(ctx)).To(BeEmpty())
		})

		It("Should create segment secret with CR write key and default API URL", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			// Update to add segment key (default spec is empty)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "test-write-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the segment secret was created with correct data")
			name := expectedSecretName("test-write-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("test-write-key"))
			Expect(string(secret.Data[segmentKeyAPIURL])).To(Equal(konfluxv1alpha1.DefaultSegmentAPIURL))
		})

		It("Should create segment secret with CR write key and custom API URL", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with a write key and custom API URL")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "test-write-key"
			segmentBridgeCR.Spec.SegmentAPIURL = "https://console.redhat.com/connections/api/v1"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the segment secret was created with custom API URL")
			name := expectedSecretName("test-write-key", "https://console.redhat.com/connections/api/v1")
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("test-write-key"))
			Expect(string(secret.Data[segmentKeyAPIURL])).To(Equal("https://console.redhat.com/connections/api/v1"))
		})

		It("Should use build-time default key when CR key is empty", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR without a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			By("configuring a build-time default key")
			reconciler.GetDefaultSegmentKey = staticSegmentKey("build-time-key")

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the segment secret uses the build-time default key")
			name := expectedSecretName("build-time-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("build-time-key"))
		})

		It("Should prefer CR key over build-time default", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "cr-override-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("configuring a build-time default key")
			reconciler.GetDefaultSegmentKey = staticSegmentKey("build-time-key")

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the segment secret uses the CR key, not the build-time default")
			name := expectedSecretName("cr-override-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: uiNamespace,
			}, secret)).To(Succeed())

			Expect(string(secret.Data[segmentKeyWriteKey])).To(Equal("cr-override-key"))
		})

		It("Should create a new secret and clean up old one when segment key changes", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with an initial write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "initial-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling to create the initial secret")
			reconcileUI(ctx)

			initialName := expectedSecretName("initial-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      initialName,
				Namespace: uiNamespace,
			}, &corev1.Secret{})).To(Succeed())

			By("changing the segment key")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "updated-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the new secret was created")
			updatedName := expectedSecretName("updated-key", konfluxv1alpha1.DefaultSegmentAPIURL)
			Expect(updatedName).NotTo(Equal(initialName), "hash should differ for different keys")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      updatedName,
				Namespace: uiNamespace,
			}, &corev1.Secret{})).To(Succeed())

			By("verifying the old secret was cleaned up")
			err := k8sClient.Get(ctx, types.NamespacedName{
				Name:      initialName,
				Namespace: uiNamespace,
			}, &corev1.Secret{})
			Expect(errors.IsNotFound(err)).To(BeTrue(), "old segment secret should be deleted")
		})

		It("Should clean up segment secret when key becomes empty", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "temporary-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling to create the secret")
			reconcileUI(ctx)
			Expect(listSegmentSecrets(ctx)).To(HaveLen(1))

			By("removing the segment key")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = ""
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling again")
			reconcileUI(ctx)

			By("verifying the secret was cleaned up")
			Expect(listSegmentSecrets(ctx)).To(BeEmpty())
		})

		It("Should set owner reference on created segment secret", func(ctx context.Context) {
			By("creating a KonfluxSegmentBridge CR with a write key")
			segmentBridgeCR = &konfluxv1alpha1.KonfluxSegmentBridge{
				ObjectMeta: metav1.ObjectMeta{
					Name: segmentbridge.CRName,
				},
			}
			Expect(k8sClient.Create(ctx, segmentBridgeCR)).To(Succeed())

			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: segmentbridge.CRName}, segmentBridgeCR)).To(Succeed())
			segmentBridgeCR.Spec.SegmentKey = "owner-ref-test-key"
			Expect(k8sClient.Update(ctx, segmentBridgeCR)).To(Succeed())

			By("reconciling the resource")
			reconcileUI(ctx)

			By("verifying the secret has owner reference")
			secrets := listSegmentSecrets(ctx)
			Expect(secrets).To(HaveLen(1))
			Expect(secrets[0].OwnerReferences).To(HaveLen(1))
			Expect(secrets[0].OwnerReferences[0].Name).To(Equal(CRName))
			Expect(secrets[0].OwnerReferences[0].Kind).To(Equal("KonfluxUI"))
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
