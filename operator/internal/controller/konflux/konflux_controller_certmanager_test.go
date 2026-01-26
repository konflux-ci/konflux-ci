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

package konflux

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

var _ = Describe("Konflux Controller - Cert-Manager Dependency", func() {
	Context("When cert-manager CRDs are not installed", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)

			// Create a fake client WITHOUT cert-manager resources
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				Build()

			// Create clusterInfo without cert-manager resources
			mockDiscoveryClient := &certManagerMockDiscoveryClient{
				hasCertManager: false,
			}
			clusterInfo, _ := clusterinfo.DetectWithClient(mockDiscoveryClient)

			reconciler = &KonfluxReconciler{
				Client:      fakeClient,
				Scheme:      scheme,
				ClusterInfo: clusterInfo,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should set CertManagerAvailable and Ready conditions to False when cert-manager is missing", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying CertManagerAvailable condition is False")
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(condition.ReasonCertManagerNotInstalled))
			Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))

			By("Verifying Ready condition is False")
			readyCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeReady)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(condition.ReasonCertManagerNotInstalled))
			Expect(readyCond.Message).To(ContainSubstring("cert-manager CRDs are not installed"))
		})
	})

	Context("When cert-manager CRDs are installed", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)

			// Create a fake client WITH cert-manager resources
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				Build()

			// Create clusterInfo with cert-manager resources
			mockDiscoveryClient := &certManagerMockDiscoveryClient{
				hasCertManager: true,
			}
			clusterInfo, _ := clusterinfo.DetectWithClient(mockDiscoveryClient)

			reconciler = &KonfluxReconciler{
				Client:      fakeClient,
				Scheme:      scheme,
				ClusterInfo: clusterInfo,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should set CertManagerAvailable condition to True", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying CertManagerAvailable condition is True")
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal("CertManagerInstalled"))
			Expect(cond.Message).To(ContainSubstring("cert-manager CRDs are installed"))
		})

		It("should not override Ready condition when cert-manager is available", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying Ready condition is not overridden by cert-manager check")
			readyCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeReady)
			// Ready condition should be set based on sub-CR statuses, not cert-manager
			// If cert-manager is available, it shouldn't force Ready to False
			if readyCond != nil {
				Expect(readyCond.Reason).NotTo(Equal(condition.ReasonCertManagerNotInstalled))
			}
		})
	})

	Context("When cert-manager check fails with an error", func() {
		var (
			ctx                context.Context
			reconciler         *KonfluxReconciler
			fakeClient         client.Client
			konflux            *konfluxv1alpha1.Konflux
			typeNamespacedName types.NamespacedName
		)

		BeforeEach(func() {
			ctx = context.Background()
			scheme := runtime.NewScheme()
			_ = konfluxv1alpha1.AddToScheme(scheme)

			// Create a fake client
			fakeClient = fake.NewClientBuilder().
				WithScheme(scheme).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				Build()

			// Create clusterInfo that returns an error when checking cert-manager
			// This simulates RBAC issues or network problems
			mockDiscoveryClient := &certManagerMockDiscoveryClient{
				hasCertManager: false,
				returnError:    true,
			}
			clusterInfo, _ := clusterinfo.DetectWithClient(mockDiscoveryClient)

			reconciler = &KonfluxReconciler{
				Client:      fakeClient,
				Scheme:      scheme,
				ClusterInfo: clusterInfo,
			}

			konflux = &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
				},
			}

			typeNamespacedName = types.NamespacedName{
				Name: CRName,
			}

			// Create the Konflux CR
			Expect(fakeClient.Create(ctx, konflux)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup
			_ = fakeClient.Delete(ctx, konflux)
		})

		It("should continue reconciliation and set CertManagerAvailable to Unknown", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			// Reconciliation should continue even if cert-manager check fails
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying that CertManagerAvailable condition is set to Unknown")
			cond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionUnknown))
			Expect(cond.Reason).To(Equal(condition.ReasonCertManagerInstallationCheckFailed))
			Expect(cond.Message).To(ContainSubstring("simulated RBAC or network error"))
		})

		It("should allow Ready to be True when CertManagerAvailable is Unknown", func() {
			By("Reconciling the Konflux CR")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Fetching the updated Konflux CR")
			updatedKonflux := &konfluxv1alpha1.Konflux{}
			Expect(fakeClient.Get(ctx, typeNamespacedName, updatedKonflux)).To(Succeed())

			By("Verifying CertManagerAvailable is Unknown")
			certManagerCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeCertManagerAvailable)
			Expect(certManagerCond).NotTo(BeNil())
			Expect(certManagerCond.Status).To(Equal(metav1.ConditionUnknown))

			By("Verifying Ready condition is not overridden by Unknown CertManagerAvailable")
			// Ready should be based on sub-CR statuses, not blocked by Unknown cert-manager status
			readyCond := apimeta.FindStatusCondition(updatedKonflux.GetConditions(), constant.ConditionTypeReady)
			// Ready condition should exist (set by SetAggregatedReadyCondition)
			// It should NOT be False with CertManagerNotInstalled reason since cert-manager is Unknown, not False
			if readyCond != nil {
				Expect(readyCond.Reason).NotTo(Equal(condition.ReasonCertManagerNotInstalled))
			}
		})
	})
})

// certManagerMockDiscoveryClient implements clusterinfo.DiscoveryClient for testing cert-manager scenarios
type certManagerMockDiscoveryClient struct {
	hasCertManager bool
	returnError    bool
}

func (m *certManagerMockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if m.returnError && groupVersion == "cert-manager.io/v1" {
		return nil, apierrors.NewInternalError(errors.New("simulated RBAC or network error"))
	}

	if groupVersion == "cert-manager.io/v1" && m.hasCertManager {
		return &metav1.APIResourceList{
			GroupVersion: "cert-manager.io/v1",
			APIResources: []metav1.APIResource{
				{Kind: "Certificate"},
				{Kind: "Issuer"},
				{Kind: "ClusterIssuer"},
			},
		}, nil
	}

	if groupVersion == "config.openshift.io/v1" {
		// Return empty to indicate not OpenShift
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
	}

	return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *certManagerMockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{GitVersion: "v1.30.0"}, nil
}
