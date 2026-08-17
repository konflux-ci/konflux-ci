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

package defaulttenant

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

var _ = Describe("Secret copy helpers", func() {
	buildTrackingClient := func(cl client.Client, dt *konfluxv1alpha1.KonfluxDefaultTenant) *tracking.Client {
		return tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
			Owner:             dt,
			OwnerLabelKey:     constant.KonfluxOwnerLabel,
			ComponentLabelKey: constant.KonfluxComponentLabel,
			Component:         string(manifests.DefaultTenant),
			FieldManager:      FieldManager,
		})
	}

	reconcilerWithObjectStore := func(cl client.Client, scheme *runtime.Scheme) *KonfluxDefaultTenantReconciler {
		return &KonfluxDefaultTenantReconciler{
			Client:      cl,
			Scheme:      scheme,
			ObjectStore: objectStore,
		}
	}

	expectDefaultTenantScheme := func(scheme *runtime.Scheme) {
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())
	}

	newDefaultTenant := func() *konfluxv1alpha1.KonfluxDefaultTenant {
		return &konfluxv1alpha1.KonfluxDefaultTenant{ObjectMeta: metav1.ObjectMeta{Name: CRName}}
	}

	newInternalRegistry := func() *konfluxv1alpha1.KonfluxInternalRegistry {
		return &konfluxv1alpha1.KonfluxInternalRegistry{ObjectMeta: metav1.ObjectMeta{Name: "konflux-internal-registry"}}
	}

	integrationRunnerSA := func() *corev1.ServiceAccount {
		return &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      IntegrationRunnerServiceAccountName,
				Namespace: DefaultTenantNamespace,
			},
		}
	}

	It("does nothing when internal registry CR is not present", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		res, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})

	It("removes stale registry credentials from integration runner when internal registry CR is absent", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		sa := integrationRunnerSA()
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: RegistryCredentialsSecretName}}
		sa.Secrets = []corev1.ObjectReference{{Name: RegistryCredentialsSecretName}}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, sa).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}

		Expect(r.ensureIntegrationRunnerRegistryCredsRemoved(ctx)).To(Succeed())

		updated := &corev1.ServiceAccount{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: DefaultTenantNamespace, Name: IntegrationRunnerServiceAccountName}, updated)).To(Succeed())
		Expect(updated.ImagePullSecrets).To(BeEmpty())
		Expect(updated.Secrets).To(BeEmpty())
	})

	It("copies credentials after internal registry is created later", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		expectDefaultTenantScheme(scheme)

		dt := newDefaultTenant()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, integrationRunnerSA()).Build()
		r := reconcilerWithObjectStore(cl, scheme)
		tc := buildTrackingClient(cl, dt)

		// Initial reconcile: registry CR does not exist yet.
		res, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))

		// Simulate late creation of internal registry and its generated source secret.
		Expect(cl.Create(ctx, newInternalRegistry())).To(Succeed())
		src := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("late-created-creds")},
		}
		Expect(cl.Create(ctx, src)).To(Succeed())

		Expect(r.applyManifests(ctx, tc, true)).To(Succeed())

		res, err = r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeZero())

		target := &corev1.Secret{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: DefaultTenantNamespace, Name: RegistryCredentialsSecretName}, target)).To(Succeed())
		Expect(target.Data[corev1.DockerConfigJsonKey]).To(Equal([]byte("late-created-creds")))

		saOut := &corev1.ServiceAccount{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: DefaultTenantNamespace, Name: IntegrationRunnerServiceAccountName}, saOut)).To(Succeed())
		Expect(saOut.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: RegistryCredentialsSecretName}))
		Expect(saOut.Secrets).To(ContainElement(corev1.ObjectReference{Name: RegistryCredentialsSecretName}))
	})

	It("requeues when internal registry exists but source credentials are missing", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		registry := newInternalRegistry()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, registry).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		res, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(10 * time.Second))
	})

	It("copies docker config from source secret into default-tenant", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		expectDefaultTenantScheme(scheme)

		srcData := []byte(`{"auths":{"registry.example":{"auth":"abc"}}}`)
		immutable := true
		src := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Immutable:  &immutable,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: srcData},
		}
		dt := newDefaultTenant()
		registry := newInternalRegistry()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(src, dt, registry, integrationRunnerSA()).Build()
		r := reconcilerWithObjectStore(cl, scheme)
		tc := buildTrackingClient(cl, dt)

		Expect(r.applyManifests(ctx, tc, true)).To(Succeed())

		res, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeZero())

		out := &corev1.Secret{}
		key := client.ObjectKey{Namespace: DefaultTenantNamespace, Name: RegistryCredentialsSecretName}
		Expect(cl.Get(ctx, key, out)).To(Succeed())
		Expect(out.Type).To(Equal(corev1.SecretTypeDockerConfigJson))
		Expect(out.Data[corev1.DockerConfigJsonKey]).To(Equal(srcData))
		Expect(out.Immutable).To(BeNil(), "copied target secret should stay mutable for rotation")

		saOut := &corev1.ServiceAccount{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: DefaultTenantNamespace, Name: IntegrationRunnerServiceAccountName}, saOut)).To(Succeed())
		Expect(saOut.ImagePullSecrets).To(ContainElement(corev1.LocalObjectReference{Name: RegistryCredentialsSecretName}))
		Expect(saOut.Secrets).To(ContainElement(corev1.ObjectReference{Name: RegistryCredentialsSecretName}))
	})

	It("updates the copied target secret when source credentials rotate", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		src := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("v1")},
		}
		dt := newDefaultTenant()
		registry := newInternalRegistry()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(src, dt, registry, integrationRunnerSA()).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		_, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		sourceOut := &corev1.Secret{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: RegistrySourceSecretNamespace, Name: RegistrySourceSecretName}, sourceOut)).To(Succeed())
		sourceOut.Data[corev1.DockerConfigJsonKey] = []byte("v2")
		Expect(cl.Update(ctx, sourceOut)).To(Succeed())

		_, err = r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())

		targetOut := &corev1.Secret{}
		Expect(cl.Get(ctx, client.ObjectKey{Namespace: DefaultTenantNamespace, Name: RegistryCredentialsSecretName}, targetOut)).To(Succeed())
		Expect(targetOut.Data[corev1.DockerConfigJsonKey]).To(Equal([]byte("v2")))
	})

	It("returns an error when reading the source secret fails with a non-NotFound error", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		registry := newInternalRegistry()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, registry).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(cctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Namespace == RegistrySourceSecretNamespace && key.Name == RegistrySourceSecretName {
					return apierrors.NewTimeoutError("apiserver", 0)
				}
				return c.Get(cctx, key, obj, opts...)
			},
		}).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		_, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get source secret"))
	})

	It("returns an error when applying the target secret fails", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		src := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"x": []byte("y")},
		}
		dt := newDefaultTenant()
		registry := newInternalRegistry()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(src, dt, registry).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(cctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if sec, ok := obj.(*corev1.Secret); ok && sec.Namespace == DefaultTenantNamespace {
					return fmt.Errorf("simulated apply failure")
				}
				return c.Patch(cctx, obj, patch, opts...)
			},
		}).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		_, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sync target secret"))
	})

	It("maps internal-registry source secret changes to default-tenant reconcile requests", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}

		match := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace}}
		reqs := r.mapSourceSecretToDefaultTenant(ctx, match)
		Expect(reqs).To(HaveLen(1))
		Expect(reqs[0].Name).To(Equal(CRName))

		other := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: RegistrySourceSecretNamespace}}
		Expect(r.mapSourceSecretToDefaultTenant(ctx, other)).To(BeEmpty())
	})

	It("returns no reconcile requests for non-secret watch objects", func() {
		ctx := context.Background()
		r := &KonfluxDefaultTenantReconciler{}

		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: RegistrySourceSecretNamespace}}
		Expect(r.mapSourceSecretToDefaultTenant(ctx, cm)).To(BeEmpty())
	})

	It("maps no requests when KonfluxDefaultTenant is absent from the client", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		match := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace}}
		Expect(r.mapSourceSecretToDefaultTenant(ctx, match)).To(BeEmpty())
	})

	It("returns an error when get internal registry fails with a non-NotFound error", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(cctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*konfluxv1alpha1.KonfluxInternalRegistry); ok && key.Name == internalregistry.CRName && key.Namespace == "" {
					return apierrors.NewTimeoutError("apiserver", 0)
				}
				return c.Get(cctx, key, obj, opts...)
			},
		}).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}
		tc := buildTrackingClient(cl, dt)

		_, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get internal registry"))
	})

	It("returns an error when removing runner creds but get serviceaccount fails", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(cctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.ServiceAccount); ok &&
					key.Namespace == DefaultTenantNamespace && key.Name == IntegrationRunnerServiceAccountName {
					return apierrors.NewTimeoutError("apiserver", 0)
				}
				return c.Get(cctx, key, obj, opts...)
			},
		}).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}

		err := r.ensureIntegrationRunnerRegistryCredsRemoved(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("get serviceaccount"))
	})

	It("succeeds when patch returns NotFound (serviceaccount deleted after get)", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		dt := newDefaultTenant()
		sa := integrationRunnerSA()
		sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: RegistryCredentialsSecretName}}
		sa.Secrets = []corev1.ObjectReference{{Name: RegistryCredentialsSecretName}}

		gr := schema.GroupResource{Group: "", Resource: "serviceaccounts"}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, sa).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(cctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if s, ok := obj.(*corev1.ServiceAccount); ok &&
					s.Namespace == DefaultTenantNamespace && s.Name == IntegrationRunnerServiceAccountName {
					return apierrors.NewNotFound(gr, IntegrationRunnerServiceAccountName)
				}
				return c.Patch(cctx, obj, patch, opts...)
			},
		}).Build()
		r := &KonfluxDefaultTenantReconciler{Client: cl, Scheme: scheme}

		Expect(r.ensureIntegrationRunnerRegistryCredsRemoved(ctx)).To(Succeed())
	})

	It("returns an error when applyManifests fails to patch integration runner for registry creds", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		expectDefaultTenantScheme(scheme)

		dt := newDefaultTenant()
		registry := newInternalRegistry()
		src := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: RegistrySourceSecretName, Namespace: RegistrySourceSecretNamespace},
			Type:       corev1.SecretTypeDockerConfigJson,
			Data:       map[string][]byte{corev1.DockerConfigJsonKey: []byte("{}")},
		}
		sa := integrationRunnerSA()

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, registry, src, sa).WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(cctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				if s, ok := obj.(*corev1.ServiceAccount); ok &&
					s.Namespace == DefaultTenantNamespace && s.Name == IntegrationRunnerServiceAccountName {
					return fmt.Errorf("simulated SA patch failure")
				}
				return c.Patch(cctx, obj, patch, opts...)
			},
		}).Build()
		r := reconcilerWithObjectStore(cl, scheme)
		tc := buildTrackingClient(cl, dt)

		err := r.applyManifests(ctx, tc, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to apply object"))
		Expect(err.Error()).To(ContainSubstring(IntegrationRunnerServiceAccountName))
	})

	It("succeeds when internal registry is absent and integration runner has no stale cred refs", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		expectDefaultTenantScheme(scheme)

		dt := newDefaultTenant()
		sa := integrationRunnerSA()

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(dt, sa).Build()
		r := reconcilerWithObjectStore(cl, scheme)
		tc := buildTrackingClient(cl, dt)

		Expect(r.applyManifests(ctx, tc, false)).To(Succeed())
		Expect(r.ensureIntegrationRunnerRegistryCredsRemoved(ctx)).To(Succeed())

		res, err := r.syncInternalRegistryCredentialsSecret(ctx, tc)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
	})
})
