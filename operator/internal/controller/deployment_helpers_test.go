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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var _ = Describe("Deployment Helpers", func() {
	var (
		deployment *appsv1.Deployment
		container  *corev1.Container
	)

	BeforeEach(func() {
		deployment = &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "test-container",
								Env: []corev1.EnvVar{
									{Name: "EXISTING_VAR", Value: "original"},
								},
								Args: []string{"--existing-arg"},
								Ports: []corev1.ContainerPort{
									{Name: "http", ContainerPort: 8080},
								},
							},
						},
					},
				},
			},
		}
		container = &deployment.Spec.Template.Spec.Containers[0]
	})

	Describe("SetEnvVar", func() {
		It("should update existing env var", func() {
			SetEnvVar(container, "EXISTING_VAR", "updated")
			Expect(container.Env).To(HaveLen(1))
			Expect(container.Env[0].Value).To(Equal("updated"))
		})

		It("should add new env var", func() {
			SetEnvVar(container, "NEW_VAR", "new-value")
			Expect(container.Env).To(HaveLen(2))
			Expect(container.Env[1].Name).To(Equal("NEW_VAR"))
			Expect(container.Env[1].Value).To(Equal("new-value"))
		})
	})

	Describe("SetContainerArg", func() {
		It("should add new arg", func() {
			added := SetContainerArg(container, "--new-arg")
			Expect(added).To(BeTrue())
			Expect(container.Args).To(ContainElement("--new-arg"))
		})

		It("should not add duplicate arg", func() {
			added := SetContainerArg(container, "--existing-arg")
			Expect(added).To(BeFalse())
			Expect(container.Args).To(HaveLen(1))
		})
	})

	Describe("SetResources", func() {
		It("should set resource requirements", func() {
			resources := corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("500m"),
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			}
			SetResources(container, resources)
			Expect(container.Resources.Requests.Cpu().String()).To(Equal("100m"))
			Expect(container.Resources.Limits.Memory().String()).To(Equal("512Mi"))
		})
	})

	Describe("SetReplicas", func() {
		It("should set replica count", func() {
			SetReplicas(deployment, 3)
			Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
		})
	})

	Describe("GetFirstContainer", func() {
		It("should return first container", func() {
			c := GetFirstContainer(deployment)
			Expect(c).NotTo(BeNil())
			Expect(c.Name).To(Equal("test-container"))
		})

		It("should return nil for empty containers", func() {
			deployment.Spec.Template.Spec.Containers = nil
			c := GetFirstContainer(deployment)
			Expect(c).To(BeNil())
		})
	})

	Describe("GetContainerByName", func() {
		It("should return container by name", func() {
			c := GetContainerByName(deployment, "test-container")
			Expect(c).NotTo(BeNil())
			Expect(c.Name).To(Equal("test-container"))
		})

		It("should return nil for non-existent container", func() {
			c := GetContainerByName(deployment, "non-existent")
			Expect(c).To(BeNil())
		})
	})

	Describe("AddContainerPort", func() {
		It("should add new port", func() {
			added := AddContainerPort(container, corev1.ContainerPort{
				Name:          "metrics",
				ContainerPort: 9100,
			})
			Expect(added).To(BeTrue())
			Expect(container.Ports).To(HaveLen(2))
		})

		It("should not add duplicate port name", func() {
			added := AddContainerPort(container, corev1.ContainerPort{
				Name:          "http",
				ContainerPort: 9000,
			})
			Expect(added).To(BeFalse())
			Expect(container.Ports).To(HaveLen(1))
		})
	})
})

// TestDeploymentHelpers runs the Ginkgo specs
func TestDeploymentHelpers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Deployment Helpers Suite")
}

