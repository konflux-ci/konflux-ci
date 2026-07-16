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
	"testing"

	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

func getSegmentBridgeCronJob(t *testing.T) *batchv1.CronJob {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.SegmentBridge)
	if err != nil {
		t.Fatalf("failed to get SegmentBridge manifests: %v", err)
	}

	for _, obj := range objects {
		if cj, ok := obj.(*batchv1.CronJob); ok && cj.Name == cronJobName {
			return cj
		}
	}
	t.Fatalf("CronJob %q not found in SegmentBridge manifests", cronJobName)
	return nil
}

func TestApplySegmentBridgeCronJobCustomizations(t *testing.T) {
	t.Run("nil CronJob spec leaves manifest unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{}

		cj := getSegmentBridgeCronJob(t)
		container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		originalLimits := container.Resources.Limits.Cpu().String()
		originalEnvFrom := container.EnvFrom

		err := applySegmentBridgeCronJobCustomizations(cj, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container = testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		g.Expect(container.Resources.Limits.Cpu().String()).To(gomega.Equal(originalLimits))
		g.Expect(container.EnvFrom).To(gomega.Equal(originalEnvFrom))
	})

	t.Run("resource overrides are applied", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{
			CronJob: &konfluxv1alpha1.ContainerSpec{
				Resources: &corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			},
		}

		cj := getSegmentBridgeCronJob(t)
		err := applySegmentBridgeCronJobCustomizations(cj, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		g.Expect(container.Resources.Limits.Cpu().String()).To(gomega.Equal("500m"))
		g.Expect(container.Resources.Limits.Memory().String()).To(gomega.Equal("1Gi"))
		g.Expect(container.Resources.Requests.Cpu().String()).To(gomega.Equal("100m"))
		g.Expect(container.Resources.Requests.Memory().String()).To(gomega.Equal("128Mi"))
	})

	t.Run("env overrides are applied without clobbering the envFrom Secret reference", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{
			CronJob: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{
					{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"},
				},
			},
		}

		cj := getSegmentBridgeCronJob(t)
		container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		originalEnvFrom := container.EnvFrom
		g.Expect(originalEnvFrom).NotTo(gomega.BeEmpty(), "test fixture must have an envFrom Secret reference")

		err := applySegmentBridgeCronJobCustomizations(cj, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container = testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		g.Expect(container.Env).To(gomega.ContainElement(
			corev1.EnvVar{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"}))
		g.Expect(container.EnvFrom).To(gomega.Equal(originalEnvFrom),
			"envFrom Secret reference providing SEGMENT_WRITE_KEY/SEGMENT_BATCH_API/TEKTON_RESULTS_API_ADDR must be untouched")
	})

	t.Run("errors when the segment-bridge container is missing from the CronJob", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{
			CronJob: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{{Name: "HTTP_PROXY", Value: "http://proxy.example.com:3128"}},
			},
		}

		cj := getSegmentBridgeCronJob(t)
		container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		container.Name = "renamed-container"

		err := applySegmentBridgeCronJobCustomizations(cj, spec)
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(
			`container "segment-bridge" not found`)))
	})

	t.Run("an env override reusing a Secret-sourced name takes precedence at runtime", func(t *testing.T) {
		// Documents current, deliberate behavior: ContainerSpec.Env uses standard
		// Kubernetes env-vs-envFrom precedence, so an override named after one of the
		// controller-managed Secret keys will shadow it. There is no admission-time
		// guard against this (consistent with every other ContainerSpec consumer in
		// this repo) -- this test exists so a future change to that behavior is
		// caught deliberately rather than accidentally.
		g := gomega.NewWithT(t)
		spec := konfluxv1alpha1.KonfluxSegmentBridgeSpec{
			CronJob: &konfluxv1alpha1.ContainerSpec{
				Env: []corev1.EnvVar{{Name: "SEGMENT_WRITE_KEY", Value: "attacker-supplied-value"}},
			},
		}

		cj := getSegmentBridgeCronJob(t)
		err := applySegmentBridgeCronJobCustomizations(cj, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(cj.Spec.JobTemplate.Spec.Template.Spec.Containers, cronJobContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		g.Expect(container.Env).To(gomega.ContainElement(
			corev1.EnvVar{Name: "SEGMENT_WRITE_KEY", Value: "attacker-supplied-value"}))
		g.Expect(container.EnvFrom).NotTo(gomega.BeEmpty(),
			"envFrom reference itself is still present even though its SEGMENT_WRITE_KEY value would be shadowed")
	})
}
