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

func TestAuthGroupsHeaderWithRealManifest(t *testing.T) {
	t.Run("embedded manifest does not contain AUTH_GROUPS_HEADER", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(found).To(gomega.BeFalse(), "AUTH_GROUPS_HEADER should not be in the embedded base manifest")
	})

	t.Run("field set injects env var into real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthGroupsHeader: "X-Group",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-Group"))
	})

	t.Run("field omitted leaves env var absent in real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(found).To(gomega.BeFalse(), "upstream default should apply — env var should be absent")
	})

	t.Run("field overrides same var set via ContainerSpec.Env on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthGroupsHeader: "X-Forwarded-Groups",
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envAuthGroupsHeader, Value: "X-Remote-Groups"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-Forwarded-Groups"), "typed field should win over ContainerSpec.Env")
	})

	t.Run("ContainerSpec.Env passes through when field omitted on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envAuthGroupsHeader, Value: "X-Remote-Groups"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-Remote-Groups"), "ContainerSpec.Env should pass through")
	})
}

func TestBothAuthHeadersWithRealManifest(t *testing.T) {
	t.Run("both set injects both env vars", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthUsernameHeader: "X-User",
			AuthGroupsHeader:   "X-Group",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		uVal, uFound := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(uFound).To(gomega.BeTrue())
		g.Expect(uVal).To(gomega.Equal("X-User"))
		gVal, gFound := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(gFound).To(gomega.BeTrue())
		g.Expect(gVal).To(gomega.Equal("X-Group"))
	})

	t.Run("both omitted leaves both env vars absent", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, uFound := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(uFound).To(gomega.BeFalse())
		_, gFound := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(gFound).To(gomega.BeFalse())
	})

	t.Run("only username set injects only username env var", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthUsernameHeader: "X-User",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		uVal, uFound := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(uFound).To(gomega.BeTrue())
		g.Expect(uVal).To(gomega.Equal("X-User"))
		_, gFound := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(gFound).To(gomega.BeFalse())
	})

	t.Run("only groups set injects only groups env var", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthGroupsHeader: "X-Group",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, uFound := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(uFound).To(gomega.BeFalse())
		gVal, gFound := findEnvValue(container.Env, envAuthGroupsHeader)
		g.Expect(gFound).To(gomega.BeTrue())
		g.Expect(gVal).To(gomega.Equal("X-Group"))
	})
}

func TestAuthUsernameHeaderWithRealManifest(t *testing.T) {
	t.Run("embedded manifest does not contain AUTH_USERNAME_HEADER", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(found).To(gomega.BeFalse(), "AUTH_USERNAME_HEADER should not be in the embedded base manifest")
	})

	t.Run("field set injects env var into real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthUsernameHeader: "X-User",
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-User"))
	})

	t.Run("field omitted leaves env var absent in real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		_, found := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(found).To(gomega.BeFalse(), "upstream default should apply — env var should be absent")
	})

	t.Run("field overrides same var set via ContainerSpec.Env on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			AuthUsernameHeader: "X-Forwarded-User",
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envAuthUsernameHeader, Value: "X-Remote-User"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-Forwarded-User"), "typed field should win over ContainerSpec.Env")
	})

	t.Run("ContainerSpec.Env passes through when field omitted on real manifest", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := getNamespaceListerDeployment(t)
		spec := konfluxv1alpha1.KonfluxNamespaceListerSpec{
			NamespaceLister: &konfluxv1alpha1.NamespaceListerDeploymentSpec{
				NamespaceLister: &konfluxv1alpha1.ContainerSpec{
					Env: []corev1.EnvVar{
						{Name: envAuthUsernameHeader, Value: "X-Remote-User"},
					},
				},
			},
		}
		err := applyNamespaceListerCustomizations(deployment, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		container := testutil.FindContainer(deployment.Spec.Template.Spec.Containers, namespaceListerContainerName)
		g.Expect(container).NotTo(gomega.BeNil())
		val, found := findEnvValue(container.Env, envAuthUsernameHeader)
		g.Expect(found).To(gomega.BeTrue())
		g.Expect(val).To(gomega.Equal("X-Remote-User"), "ContainerSpec.Env should pass through")
	})
}
