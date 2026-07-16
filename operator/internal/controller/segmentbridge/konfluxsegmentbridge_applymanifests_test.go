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

package segmentbridge

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

type fakeManifestSource struct {
	objects []client.Object
	err     error
}

func (f *fakeManifestSource) GetForComponent(_ manifests.Component) ([]client.Object, error) {
	return f.objects, f.err
}

func TestApplyManifestsGetForComponentFailure(t *testing.T) {
	g := gomega.NewWithT(t)

	r := &KonfluxSegmentBridgeReconciler{
		ObjectStore: &fakeManifestSource{err: errors.New("boom")},
	}

	err := r.applyManifests(context.Background(), nil, konfluxv1alpha1.KonfluxSegmentBridgeSpec{})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to get parsed manifests for SegmentBridge")))
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("boom")))
}

func TestApplyManifestsCronJobCustomizationFailure(t *testing.T) {
	g := gomega.NewWithT(t)

	cj := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{Name: cronJobName},
		Spec: batchv1.CronJobSpec{
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "something-else"},
							},
						},
					},
				},
			},
		},
	}

	r := &KonfluxSegmentBridgeReconciler{
		ObjectStore: &fakeManifestSource{objects: []client.Object{cj}},
	}

	spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{
		CronJob: &konfluxv1alpha1.ContainerSpec{
			Env: []corev1.EnvVar{{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"}},
		},
	}

	err := r.applyManifests(context.Background(), nil, spec)
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to apply customizations to CronJob")))
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(cronJobName)))
}

func TestApplyManifestsApplyOwnedFailure(t *testing.T) {
	g := gomega.NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
	g.Expect(konfluxv1alpha1.AddToScheme(scheme)).To(gomega.Succeed())

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "segment-bridge-config-map", Namespace: "segment-bridge"},
	}

	owner := &konfluxv1alpha1.KonfluxSegmentBridge{ObjectMeta: metav1.ObjectMeta{Name: CRName}}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).WithInterceptorFuncs(interceptor.Funcs{
		Patch: func(cctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			if _, ok := obj.(*corev1.ConfigMap); ok {
				return fmt.Errorf("simulated apply failure")
			}
			return c.Patch(cctx, obj, patch, opts...)
		},
	}).Build()

	tc := tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         string(manifests.SegmentBridge),
		FieldManager:      FieldManager,
	})

	r := &KonfluxSegmentBridgeReconciler{
		Client:      cl,
		Scheme:      scheme,
		ObjectStore: &fakeManifestSource{objects: []client.Object{cm}},
	}

	err := r.applyManifests(context.Background(), tc, konfluxv1alpha1.KonfluxSegmentBridgeSpec{})
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("failed to apply object")))
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(cm.Name)))
	g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("simulated apply failure")))
}
