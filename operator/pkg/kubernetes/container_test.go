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
)

func TestFindContainer(t *testing.T) {
	containers := []corev1.Container{
		{Name: "sidecar"},
		{Name: "manager"},
	}

	t.Run("returns pointer to matching container", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := FindContainer(containers, "manager")
		g.Expect(result).NotTo(gomega.BeNil())
		g.Expect(result.Name).To(gomega.Equal("manager"))
		// Verify it returns a pointer into the original slice, not a copy.
		g.Expect(result).To(gomega.BeIdenticalTo(&containers[1]))
	})

	t.Run("returns nil when not found", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindContainer(containers, "does-not-exist")).To(gomega.BeNil())
	})

	t.Run("returns nil for empty slice", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(FindContainer(nil, "manager")).To(gomega.BeNil())
	})
}
