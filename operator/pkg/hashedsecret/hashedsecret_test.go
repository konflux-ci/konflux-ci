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

package hashedsecret

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestBuild(t *testing.T) {
	const (
		baseName  = "my-secret"
		namespace = "test-ns"
	)

	t.Run("creates secret with hashed name and correct data", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := map[string]string{"key": "write-key", "url": "https://api.segment.io/v1"}
		secret := Build(baseName, namespace, data)

		g.Expect(secret.Name).To(gomega.HavePrefix(baseName + "-"))
		g.Expect(secret.Namespace).To(gomega.Equal(namespace))
		g.Expect(secret.StringData).To(gomega.Equal(data))
	})

	t.Run("has correct TypeMeta for server-side apply", func(t *testing.T) {
		g := gomega.NewWithT(t)

		secret := Build(baseName, namespace, map[string]string{"k": "v"})

		g.Expect(secret.APIVersion).To(gomega.Equal("v1"))
		g.Expect(secret.Kind).To(gomega.Equal("Secret"))
	})

	t.Run("different content produces different names", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s1 := Build(baseName, namespace, map[string]string{"k": "a"})
		s2 := Build(baseName, namespace, map[string]string{"k": "b"})

		g.Expect(s1.Name).NotTo(gomega.Equal(s2.Name))
	})

	t.Run("same content produces same name", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s1 := Build(baseName, namespace, map[string]string{"k": "v"})
		s2 := Build(baseName, namespace, map[string]string{"k": "v"})

		g.Expect(s1.Name).To(gomega.Equal(s2.Name))
	})

	t.Run("hash suffix has expected length", func(t *testing.T) {
		g := gomega.NewWithT(t)

		secret := Build(baseName, namespace, map[string]string{"k": "v"})
		suffix := secret.Name[len(baseName)+1:] // strip "baseName-"

		g.Expect(suffix).To(gomega.HaveLen(10))
	})

	t.Run("key order does not affect the hash", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s1 := Build(baseName, namespace, map[string]string{"a": "1", "b": "2"})
		s2 := Build(baseName, namespace, map[string]string{"b": "2", "a": "1"})

		g.Expect(s1.Name).To(gomega.Equal(s2.Name))
	})

	t.Run("different namespaces do not affect the hash", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s1 := Build(baseName, "ns-a", map[string]string{"k": "v"})
		s2 := Build(baseName, "ns-b", map[string]string{"k": "v"})

		// Same data → same hash suffix, but different namespace field
		g.Expect(s1.Name).To(gomega.Equal(s2.Name))
		g.Expect(s1.Namespace).NotTo(gomega.Equal(s2.Namespace))
	})
}
