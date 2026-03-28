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

package applicationapi

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(konfluxv1alpha1.AddToScheme(scheme))
	return scheme
}

func TestKonfluxApplicationAPIReconciler_Reconcile_Errors(t *testing.T) {
	scheme := setupScheme()
	ctx := context.TODO()

	t.Run("should handle client Get error gracefully", func(t *testing.T) {
		g := gomega.NewWithT(t)
		mockClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				return errors.New("simulated get error")
			},
		}).Build()

		reconciler := &KonfluxApplicationAPIReconciler{Client: mockClient, Scheme: scheme}
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-cr", Namespace: "default"}}

		_, err := reconciler.Reconcile(ctx, req)
		g.Expect(err).To(gomega.HaveOccurred())
	})

	t.Run("should handle applyManifests error in Reconcile", func(t *testing.T) {
		g := gomega.NewWithT(t)
		mockClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("simulated patch error")
			},
		}).Build()

		// Create the CR so Get() passes and it reaches applyManifests
		cr := &konfluxv1alpha1.KonfluxApplicationAPI{ObjectMeta: metav1.ObjectMeta{Name: "test-cr", Namespace: "default"}}
		mockClient.Create(ctx, cr)

		store, _ := manifests.NewObjectStore(scheme)
		reconciler := &KonfluxApplicationAPIReconciler{Client: mockClient, Scheme: scheme, ObjectStore: store}
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-cr", Namespace: "default"}}

		_, err := reconciler.Reconcile(ctx, req)
		g.Expect(err).To(gomega.HaveOccurred())
	})

	t.Run("should handle Status Update error in Reconcile", func(t *testing.T) {
		g := gomega.NewWithT(t)
		mockClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return errors.New("simulated status update error")
			},
		}).Build()

		cr := &konfluxv1alpha1.KonfluxApplicationAPI{ObjectMeta: metav1.ObjectMeta{Name: "test-cr", Namespace: "default"}}
		mockClient.Create(ctx, cr)

		store, _ := manifests.NewObjectStore(scheme)
		reconciler := &KonfluxApplicationAPIReconciler{Client: mockClient, Scheme: scheme, ObjectStore: store}
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "test-cr", Namespace: "default"}}

		_, err := reconciler.Reconcile(ctx, req)
		g.Expect(err).To(gomega.HaveOccurred())
	})
}

func TestKonfluxApplicationAPIReconciler_SetupWithManager(t *testing.T) {
	g := gomega.NewWithT(t)
	scheme := setupScheme()
	store, _ := manifests.NewObjectStore(scheme)

	reconciler := &KonfluxApplicationAPIReconciler{
		Client:      fake.NewClientBuilder().WithScheme(scheme).Build(),
		Scheme:      scheme,
		ObjectStore: store,
	}

	err := reconciler.SetupWithManager(nil)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestKonfluxApplicationAPIReconciler_applyManifests(t *testing.T) {
	t.Run("fails when tracking client fails to apply", func(t *testing.T) {
		g := gomega.NewWithT(t)
		scheme := setupScheme()

		mockClient := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, client client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return errors.New("simulated tracking error")
			},
		}).Build()

		store, _ := manifests.NewObjectStore(scheme)
		reconciler := &KonfluxApplicationAPIReconciler{Client: mockClient, Scheme: scheme, ObjectStore: store}
		tc := tracking.NewClientWithOwnership(mockClient, tracking.OwnershipConfig{Owner: &konfluxv1alpha1.KonfluxApplicationAPI{}})

		err := reconciler.applyManifests(context.TODO(), tc)
		g.Expect(err).To(gomega.HaveOccurred())
	})
}
