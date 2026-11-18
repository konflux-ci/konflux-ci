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
	"errors"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	consolev1 "github.com/openshift/api/console/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/consolelink"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

// A ConsoleLink lookup that fails for a reason other than NoMatch — discovery
// briefly unavailable, RBAC — has not established that the CRD is absent. Unit
// paths that do not run through the envtest manager (see PR #6336).
var _ = Describe("KonfluxUI ConsoleLink lookup failures (unit)", func() {
	var errDiscovery = errors.New("discovery unavailable")

	// newReconciler returns a reconciler on an OpenShift cluster whose client
	// resolves every REST mapping with mapperErr.
	newReconciler := func(mapperErr error) (*KonfluxUIReconciler, client.Client) {
		scheme := runtime.NewScheme()
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())
		Expect(networkingv1.AddToScheme(scheme)).To(Succeed())
		Expect(consolev1.AddToScheme(scheme)).To(Succeed())

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithRESTMapper(failingRESTMapper{err: mapperErr}).
			Build()

		openShiftInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
			resources: map[string]*metav1.APIResourceList{
				"config.openshift.io/v1": {
					APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
				},
			},
			serverVersion: &version.Info{GitVersion: "v1.29.0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(openShiftInfo.IsOpenShift()).To(BeTrue())

		return &KonfluxUIReconciler{
			Client:      cl,
			Scheme:      scheme,
			ClusterInfo: openShiftInfo,
		}, cl
	}

	reconcileIngressWith := func(ctx context.Context, r *KonfluxUIReconciler, cl client.Client) error {
		ui := &konfluxv1alpha1.KonfluxUI{ObjectMeta: metav1.ObjectMeta{Name: CRName, UID: types.UID("uid-1")}}
		tc := tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
			Owner:             ui,
			OwnerLabelKey:     constant.KonfluxOwnerLabel,
			ComponentLabelKey: constant.KonfluxComponentLabel,
			Component:         string(manifests.UI),
			FieldManager:      FieldManager,
		})
		endpoint, err := url.Parse("https://konflux.apps.example.com")
		Expect(err).NotTo(HaveOccurred())

		return r.reconcileIngress(ctx, tc, ui, endpoint)
	}

	// Treating the failure as absence would leave the applied ConsoleLink
	// untracked, and CleanupOrphans — which runs after reconcileIngress returns
	// nil — would then delete the live link as an orphan.
	It("fails the reconcile instead of treating the lookup failure as an absent CRD", func(ctx context.Context) {
		r, cl := newReconciler(errDiscovery)

		err := reconcileIngressWith(ctx, r, cl)
		Expect(err).To(MatchError(errDiscovery))
	})

	It("does not apply the Ingress when the lookup fails", func(ctx context.Context) {
		r, cl := newReconciler(errDiscovery)

		Expect(reconcileIngressWith(ctx, r, cl)).NotTo(Succeed())

		By("confirming the reconcile ended before anything was applied")
		err := cl.Get(ctx, types.NamespacedName{
			Name: ingress.IngressName, Namespace: uiNamespace,
		}, &networkingv1.Ingress{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected no Ingress, got err=%v", err)

		err = cl.Get(ctx, types.NamespacedName{
			Name: consolelink.ConsoleLinkName,
		}, &consolev1.ConsoleLink{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "expected no ConsoleLink, got err=%v", err)
	})
})

// failingRESTMapper resolves every lookup with a fixed error, standing in for a
// discovery endpoint that is briefly unavailable.
type failingRESTMapper struct {
	err error
}

func (m failingRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, m.err
}
func (m failingRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, m.err
}
func (m failingRESTMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, m.err
}
func (m failingRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, m.err
}
func (m failingRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}
func (m failingRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, m.err
}
func (m failingRESTMapper) ResourceSingularizer(_ string) (string, error) {
	return "", m.err
}
