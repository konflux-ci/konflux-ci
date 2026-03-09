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

package contenthash

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestString(t *testing.T) {
	g := gomega.NewWithT(t)

	t.Run("same input produces same hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(String("hello")).To(gomega.Equal(String("hello")))
	})

	t.Run("different input produces different hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(String("hello")).NotTo(gomega.Equal(String("world")))
	})

	t.Run("hash has expected length", func(t *testing.T) {
		g.Expect(String("anything")).To(gomega.HaveLen(suffixLength))
	})
}

func TestMap(t *testing.T) {
	t.Run("same data produces same hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		d := map[string]string{"a": "1", "b": "2"}
		g.Expect(Map(d)).To(gomega.Equal(Map(d)))
	})

	t.Run("different data produces different hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(Map(map[string]string{"a": "1"})).
			NotTo(gomega.Equal(Map(map[string]string{"a": "2"})))
	})

	t.Run("key order does not affect the hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(Map(map[string]string{"a": "1", "b": "2"})).
			To(gomega.Equal(Map(map[string]string{"b": "2", "a": "1"})))
	})

	t.Run("hash has expected length", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(Map(map[string]string{"k": "v"})).To(gomega.HaveLen(suffixLength))
	})
}
