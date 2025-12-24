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

package controller

import (
	"context"
	"encoding/base64"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

var _ = Describe("KonfluxUI Controller", func() {
	Context("When reconciling a resource", func() {

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      KonfluxUICRName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		konfluxui := &konfluxv1alpha1.KonfluxUI{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KonfluxUI")
			err := k8sClient.Get(ctx, typeNamespacedName, konfluxui)
			if err != nil && errors.IsNotFound(err) {
				resource := &konfluxv1alpha1.KonfluxUI{
					ObjectMeta: metav1.ObjectMeta{
						Name:      KonfluxUICRName,
						Namespace: "default",
					},
					// TODO(user): Specify other spec details if needed.
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &konfluxv1alpha1.KonfluxUI{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KonfluxUI")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KonfluxUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("ensureUISecrets", func() {
		var ui *konfluxv1alpha1.KonfluxUI
		var reconciler *KonfluxUIReconciler

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
					Name: KonfluxUICRName,
				},
			}
			Expect(k8sClient.Create(ctx, ui)).To(Succeed())

			reconciler = &KonfluxUIReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
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
						KonfluxOwnerLabel:     KonfluxUICRName,
						KonfluxComponentLabel: string(manifests.UI),
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

			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the secret data was preserved")
			updatedSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSecret.Data["client-secret"]).To(Equal(existingData))
		})

		It("Should update ownership labels and owner reference when secret exists but has incorrect ownership", func(ctx context.Context) {
			By("creating a secret with incorrect ownership labels and no owner reference")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "oauth2-proxy-client-secret",
					Namespace: uiNamespace,
					Labels: map[string]string{
						KonfluxOwnerLabel:     "wrong-owner",
						KonfluxComponentLabel: "wrong-component",
					},
				},
				Data: map[string][]byte{
					"client-secret": []byte("existing-value"),
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ownership labels were updated")
			updatedSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSecret.Labels).To(HaveKeyWithValue(KonfluxOwnerLabel, KonfluxUICRName))
			Expect(updatedSecret.Labels).To(HaveKeyWithValue(KonfluxComponentLabel, string(manifests.UI)))

			By("verifying the owner reference was added")
			Expect(updatedSecret.OwnerReferences).To(HaveLen(1))
			Expect(updatedSecret.OwnerReferences[0].Name).To(Equal(KonfluxUICRName))
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

			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the secret data was generated")
			updatedSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, updatedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSecret.Data).To(HaveKey("client-secret"))
			Expect(updatedSecret.Data["client-secret"]).ToNot(BeEmpty())
		})

		It("Should use URL-safe base64 encoding for client-secret", func(ctx context.Context) {
			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the client-secret uses URL-safe encoding")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			clientSecretValue := string(secret.Data["client-secret"])
			By("verifying no padding characters (URL-safe uses RawURLEncoding)")
			Expect(clientSecretValue).NotTo(ContainSubstring("="))
			By("verifying it contains only URL-safe characters")
			Expect(clientSecretValue).To(MatchRegexp("^[A-Za-z0-9_-]+$"))
		})

		It("Should use standard base64 encoding for cookie-secret", func(ctx context.Context) {
			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the cookie-secret uses standard encoding")
			secret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-cookie-secret",
				Namespace: uiNamespace,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			cookieSecretValue := string(secret.Data["cookie-secret"])
			By("verifying it contains standard base64 characters (may include + and /)")
			Expect(cookieSecretValue).To(MatchRegexp("^[A-Za-z0-9+/]+=*$"))
		})

		It("Should create both secrets in a single call", func(ctx context.Context) {
			By("calling ensureUISecrets")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("verifying both secrets were created")
			clientSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, clientSecret)
			Expect(err).NotTo(HaveOccurred())

			cookieSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-cookie-secret",
				Namespace: uiNamespace,
			}, cookieSecret)
			Expect(err).NotTo(HaveOccurred())
		})

		It("Should be idempotent when called multiple times", func(ctx context.Context) {
			By("calling ensureUISecrets first time")
			err := reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

			By("getting the first secret value")
			secret1 := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      "oauth2-proxy-client-secret",
				Namespace: uiNamespace,
			}, secret1)
			Expect(err).NotTo(HaveOccurred())
			firstValue := secret1.Data["client-secret"]

			By("calling ensureUISecrets second time")
			err = reconciler.ensureUISecrets(ctx, ui)
			Expect(err).NotTo(HaveOccurred())

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
})
