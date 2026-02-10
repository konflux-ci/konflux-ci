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

package kubernetes

import (
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsCustomResourceDefinition(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("returns true for CustomResourceDefinition", func(t *testing.T) {
		crd := &apiextensionsv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "apiextensions.k8s.io/v1",
				Kind:       "CustomResourceDefinition",
			},
			ObjectMeta: metav1.ObjectMeta{Name: "applications.appstudio.redhat.com"},
		}
		g.Expect(IsCustomResourceDefinition(crd)).To(gomega.BeTrue())
	})

	t.Run("returns false for ConfigMap", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		g.Expect(IsCustomResourceDefinition(cm)).To(gomega.BeFalse())
	})

	t.Run("returns true for CustomResourceDefinition with empty GVK (type assertion fallback)",
		func(t *testing.T) {
			crd := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: "applications.appstudio.redhat.com"},
			}
			crd.APIVersion = ""
			crd.Kind = ""
			g.Expect(IsCustomResourceDefinition(crd)).To(gomega.BeTrue())
		})

	t.Run("returns false for empty GVK", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		cm.APIVersion = ""
		cm.Kind = ""
		g.Expect(IsCustomResourceDefinition(cm)).To(gomega.BeFalse())
	})
}
