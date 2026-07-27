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

package segment

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
)

func TestResolveWriteKey(t *testing.T) {
	t.Run("returns the CR key and source when the CR key is set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("cr-key")
		g.Expect(key).To(gomega.Equal("cr-key"))
		g.Expect(source).To(gomega.Equal("cr"))
	})

	t.Run("returns empty key and source when the CR key is not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		key, source := ResolveWriteKey("")
		g.Expect(key).To(gomega.BeEmpty())
		g.Expect(source).To(gomega.BeEmpty())
	})
}

func TestLogWriteKeyResolution(t *testing.T) {
	log := logr.Discard()

	t.Run("returns false when the key is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "", "")).To(gomega.BeFalse())
	})

	t.Run("returns true when the key is present", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "some-key", "cr")).To(gomega.BeTrue())
	})
}
