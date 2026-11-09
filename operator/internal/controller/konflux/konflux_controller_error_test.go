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

// standardSpec returns a KonfluxSpec in standard installation mode.
// All standard components are enabled; the Patch/Get interceptors target
// specific kinds, so enabling all components is safe for error testing.
func standardSpec() konfluxv1alpha1.KonfluxSpec {
	return konfluxv1alpha1.KonfluxSpec{
		InstallationType: konfluxv1alpha1.InstallationTypeStandard,
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
			specFn:   standardSpec,
		},
		{
			name:     "BuildService",
			failKind: "KonfluxBuildService",
			specFn:   standardSpec,
		},
		{
			name:     "IntegrationService",
			failKind: "KonfluxIntegrationService",
			specFn:   standardSpec,
		},
		{
			name:     "ReleaseService",
			failKind: "KonfluxReleaseService",
			specFn:   standardSpec,
		},
		{
			name:     "UI",
			failKind: "KonfluxUI",
			specFn:   standardSpec,
		},
		{
			name:     "RBAC",
			failKind: "KonfluxRBAC",
			specFn:   standardSpec,
		},
		{
			name:     "Info",
			failKind: "KonfluxInfo",
			specFn:   standardSpec,
		},
		{
			name:     "NamespaceLister",
			failKind: "KonfluxNamespaceLister",
			specFn:   standardSpec,
		},
		{
			name:     "EnterpriseContract",
			failKind: "KonfluxEnterpriseContract",
			specFn:   standardSpec,
		},
		{
			name:     "CertManager",
			failKind: "KonfluxCertManager",
			specFn:   standardSpec,
		},
		{
			name:     "CLI",
			failKind: "KonfluxCLI",
			specFn:   standardSpec,
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
			specFn:     standardSpec,
		},
		{
			name:       "IntegrationService",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxIntegrationService); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "ReleaseService",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxReleaseService); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "UI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxUI); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "RBAC",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxRBAC); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "Info",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxInfo); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "NamespaceLister",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxNamespaceLister); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "EnterpriseContract",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxEnterpriseContract); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "ApplicationAPI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxApplicationAPI); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "CLI",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxCLI); return ok },
			specFn:     standardSpec,
		},
		{
			name:       "CertManager",
			failTarget: func(obj client.Object) bool { _, ok := obj.(*konfluxv1alpha1.KonfluxCertManager); return ok },
			specFn:     standardSpec,
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
