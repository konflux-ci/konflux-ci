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

package namespacelister

import (
	"testing"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// getNamespaceListerDeployment returns a deep copy of the namespace-lister deployment from the embedded manifests.
func getNamespaceListerDeployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	store := testutil.GetTestObjectStore(t)
	objects, err := store.GetForComponent(manifests.NamespaceLister)
	if err != nil {
		t.Fatalf("failed to get NamespaceLister manifests: %v", err)
	}

	for _, obj := range objects {
		if deployment, ok := obj.(*appsv1.Deployment); ok {
			if deployment.Name == "namespace-lister" {
				return deployment
			}
		}
	}
	t.Fatalf("deployment %q not found in NamespaceLister manifests", "namespace-lister")
	return nil
}

func TestLogLevelWithRealManifest(t *testing.T) {
	t.Run("embedded manifest does not contain LOG_LEVEL", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envLogLevel)
		g.Expect(found).To(gomega.BeFalse(), "LOG_LEVEL should not be in the embedded base manifest")
	})

	t.Run("field set to info injects LOG_LEVEL=0 into real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			LogLevel: konfluxv1alpha1.LogLevelInfo,
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envLogLevel)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("0"))
	})

	t.Run("all enum values map to correct slog integers on real manifest", func(t *testing.T) {
		cases := map[konfluxv1alpha1.LogLevel]string{
			konfluxv1alpha1.LogLevelDebug: "-4",
			konfluxv1alpha1.LogLevelInfo:  "0",
			konfluxv1alpha1.LogLevelWarn:  "4",
			konfluxv1alpha1.LogLevelError: "8",
		}
		for level, expected := range cases {
			t.Run(string(level), func(t *testing.T) {
				g := gomega.NewWithT(t)
				deployment := getNamespaceListerDeployment(t)
				spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
					LogLevel: level,
				}
				err := applyNamespaceListerCustomizations(deployment, spec)
				g.Expect(err).NotTo(gomega.HaveOccurred())

				container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
				g.Expect(container).NotTo(gomega.BeNil())
				val, found := findEnvValue(container.Env, envLogLevel)
				g.Expect(found).To(gomega.BeTrue())
				g.Expect(val).To(gomega.Equal(expected))
			})
		}
	})

	t.Run("field omitted leaves env var absent in real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envLogLevel)
		g.Expect(found).To(gomega.BeFalse(), "upstream default should apply — env var should be absent")
	})

	t.Run("field overrides same var set via ContainerSpec.Env on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			LogLevel: konfluxv1alpha1.LogLevelDebug,
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envLogLevel, Value: "8"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envLogLevel)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("-4"), "typed field should win over ContainerSpec.Env")
	})
}

func TestCacheResyncPeriodWithRealManifest(t *testing.T) {
	t.Run("embedded manifest does not contain CACHE_RESYNC_PERIOD", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envCacheResyncPeriod)
		g.Expect(found).To(gomega.BeFalse(), "CACHE_RESYNC_PERIOD should not be in the embedded base manifest")
	})

	t.Run("field set injects env var into real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			CacheResyncPeriod: "10m",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envCacheResyncPeriod)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("10m"))
	})

	t.Run("field omitted leaves env var absent in real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envCacheResyncPeriod)
		g.Expect(found).To(gomega.BeFalse(), "upstream default should apply — env var should be absent")
	})

	t.Run("field overrides same var set via ContainerSpec.Env on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			CacheResyncPeriod: "5m",
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envCacheResyncPeriod, Value: "30m"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envCacheResyncPeriod)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("5m"), "typed field should win over ContainerSpec.Env")
	})

	t.Run("ContainerSpec.Env passes through when field omitted on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envCacheResyncPeriod, Value: "30m"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envCacheResyncPeriod)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("30m"), "ContainerSpec.Env should pass through")
	})
}
