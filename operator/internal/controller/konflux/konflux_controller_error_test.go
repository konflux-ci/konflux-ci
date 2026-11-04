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
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// newTestScheme creates a minimal scheme for unit tests.
func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = konfluxv1alpha1.AddToScheme(s)
	return s
}

// boolPtr returns a pointer to a bool value.
func boolPtr(v bool) *bool { return &v }

// allDisabledSpec returns a KonfluxSpec with every component explicitly disabled.
func allDisabledSpec() konfluxv1alpha1.KonfluxSpec {
	return konfluxv1alpha1.KonfluxSpec{
		ApplicationAPI:            &konfluxv1alpha1.ApplicationAPIConfig{Enabled: boolPtr(false)},
		KonfluxBuildService:       &konfluxv1alpha1.BuildServiceConfig{Enabled: boolPtr(false)},
		KonfluxIntegrationService: &konfluxv1alpha1.IntegrationServiceConfig{Enabled: boolPtr(false)},
		KonfluxReleaseService:     &konfluxv1alpha1.ReleaseServiceConfig{Enabled: boolPtr(false)},
		KonfluxUI:                 &konfluxv1alpha1.KonfluxUIConfig{Enabled: boolPtr(false)},
		RBAC:                      &konfluxv1alpha1.RBACConfig{Enabled: boolPtr(false)},
		KonfluxInfo:               &konfluxv1alpha1.KonfluxInfoConfig{Enabled: boolPtr(false)},
		NamespaceLister:           &konfluxv1alpha1.NamespaceListerConfig{Enabled: boolPtr(false)},
		EnterpriseContract:        &konfluxv1alpha1.EnterpriseContractConfig{Enabled: boolPtr(false)},
		CertManager:               &konfluxv1alpha1.CertManagerConfig{Enabled: boolPtr(false)},
		DefaultTenant:             &konfluxv1alpha1.DefaultTenantConfig{Enabled: boolPtr(false)},
		CLI:                       &konfluxv1alpha1.CLIConfig{Enabled: boolPtr(false)},
	}
}

// TestReconcile_ApplyErrors verifies that each component's apply failure triggers
// errHandler.HandleWithReason and returns an error from Reconcile.
// This covers the error-return paths for the conditional apply blocks.
func TestReconcile_ApplyErrors(t *testing.T) {
	// Each test enables only the target component (and all components that come before
	// it in the Reconcile order are disabled) so the target's apply is the first one
	// that runs. When its Patch fails, HandleWithReason fires immediately.
	tests := []struct {
		name     string
		failKind string
		specFn   func() konfluxv1alpha1.KonfluxSpec
	}{
		{
			name:     "ApplicationAPI",
			failKind: "KonfluxApplicationAPI",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.ApplicationAPI = &konfluxv1alpha1.ApplicationAPIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "BuildService",
			failKind: "KonfluxBuildService",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxBuildService = &konfluxv1alpha1.BuildServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "IntegrationService",
			failKind: "KonfluxIntegrationService",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxIntegrationService = &konfluxv1alpha1.IntegrationServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "ReleaseService",
			failKind: "KonfluxReleaseService",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxReleaseService = &konfluxv1alpha1.ReleaseServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "UI",
			failKind: "KonfluxUI",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxUI = &konfluxv1alpha1.KonfluxUIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "RBAC",
			failKind: "KonfluxRBAC",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.RBAC = &konfluxv1alpha1.RBACConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "Info",
			failKind: "KonfluxInfo",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxInfo = &konfluxv1alpha1.KonfluxInfoConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "NamespaceLister",
			failKind: "KonfluxNamespaceLister",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.NamespaceLister = &konfluxv1alpha1.NamespaceListerConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "EnterpriseContract",
			failKind: "KonfluxEnterpriseContract",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.EnterpriseContract = &konfluxv1alpha1.EnterpriseContractConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "CertManager",
			failKind: "KonfluxCertManager",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.CertManager = &konfluxv1alpha1.CertManagerConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:     "CLI",
			failKind: "KonfluxCLI",
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.CLI = &konfluxv1alpha1.CLIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" apply error", func(t *testing.T) {
			g := gomega.NewWithT(t)
			s := newTestScheme()

			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
					UID:  types.UID("test-uid"),
				},
				Spec: tt.specFn(),
			}

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(konflux).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				WithInterceptorFuncs(interceptor.Funcs{
					Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
						if obj.GetObjectKind().GroupVersionKind().Kind == tt.failKind {
							return fmt.Errorf("simulated apply error for %s", tt.failKind)
						}
						return c.Patch(ctx, obj, patch, opts...)
					},
				}).Build()

			r := &KonfluxReconciler{
				Client:      cl,
				Scheme:      s,
				ClusterInfo: createTestClusterInfo(),
			}

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: CRName},
			})
			g.Expect(err).To(gomega.HaveOccurred())
			g.Expect(err.Error()).To(gomega.ContainSubstring("simulated apply error"))
		})
	}
}

// TestReconcile_StatusGetErrors verifies that each component's status-get failure
// triggers errHandler.HandleWithReason and returns an error from Reconcile.
// The apply phase uses Patch (not Get), so the Get interceptor only affects
// the status-collection phase without interfering with applies.
func TestReconcile_StatusGetErrors(t *testing.T) {
	tests := []struct {
		name       string
		failTarget func(obj client.Object) bool
		specFn     func() konfluxv1alpha1.KonfluxSpec
	}{
		{
			name:       "BuildService",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxBuildService); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxBuildService = &konfluxv1alpha1.BuildServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "IntegrationService",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxIntegrationService); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxIntegrationService = &konfluxv1alpha1.IntegrationServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "ReleaseService",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxReleaseService); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxReleaseService = &konfluxv1alpha1.ReleaseServiceConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "UI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxUI); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxUI = &konfluxv1alpha1.KonfluxUIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "RBAC",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxRBAC); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.RBAC = &konfluxv1alpha1.RBACConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "Info",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxInfo); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.KonfluxInfo = &konfluxv1alpha1.KonfluxInfoConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "NamespaceLister",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxNamespaceLister); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.NamespaceLister = &konfluxv1alpha1.NamespaceListerConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "EnterpriseContract",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxEnterpriseContract); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.EnterpriseContract = &konfluxv1alpha1.EnterpriseContractConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "ApplicationAPI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxApplicationAPI); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.ApplicationAPI = &konfluxv1alpha1.ApplicationAPIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "CLI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxCLI); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.CLI = &konfluxv1alpha1.CLIConfig{Enabled: boolPtr(true)}
				return s
			},
		},
		{
			name:       "CertManager",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxCertManager); return ok },
			specFn: func() konfluxv1alpha1.KonfluxSpec {
				s := allDisabledSpec()
				s.CertManager = &konfluxv1alpha1.CertManagerConfig{Enabled: boolPtr(true)}
				return s
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" status get error", func(t *testing.T) {
			g := gomega.NewWithT(t)
			s := newTestScheme()

			konflux := &konfluxv1alpha1.Konflux{
				ObjectMeta: metav1.ObjectMeta{
					Name: CRName,
					UID:  types.UID("test-uid"),
				},
				Spec: tt.specFn(),
			}

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(konflux).
				WithStatusSubresource(&konfluxv1alpha1.Konflux{}).
				WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if tt.failTarget(obj) {
							return fmt.Errorf("simulated get error for %s", tt.name)
						}
						return c.Get(ctx, key, obj, opts...)
					},
				}).Build()

			r := &KonfluxReconciler{
				Client:      cl,
				Scheme:      s,
				ClusterInfo: createTestClusterInfo(),
			}

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: CRName},
			})
			g.Expect(err).To(gomega.HaveOccurred())
			g.Expect(err.Error()).To(gomega.ContainSubstring("simulated get error"))
		})
	}
}
