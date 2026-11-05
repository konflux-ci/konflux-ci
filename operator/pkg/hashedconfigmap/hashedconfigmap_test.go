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

package hashedconfigmap

import (
	"testing"

	"github.com/onsi/gomega"
)

func TestGenerateHashSuffix(t *testing.T) {
	t.Run("generates consistent hash for same content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		content := "test content"
		hash1 := GenerateHashSuffix(content)
		hash2 := GenerateHashSuffix(content)
		g.Expect(hash1).To(gomega.Equal(hash2))
	})

	t.Run("generates different hash for different content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash1 := GenerateHashSuffix("content1")
		hash2 := GenerateHashSuffix("content2")
		g.Expect(hash1).NotTo(gomega.Equal(hash2))
	})

	t.Run("hash has correct length", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash := GenerateHashSuffix("any content")
		g.Expect(hash).To(gomega.HaveLen(HashSuffixLength))
	})

	t.Run("hash is hex encoded", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash := GenerateHashSuffix("test")
		g.Expect(hash).To(gomega.MatchRegexp("^[0-9a-f]+$"))
	})
}

func TestBuildConfigMapName(t *testing.T) {
	t.Run("combines base name and hash suffix", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name := BuildConfigMapName("my-config", "test content")
		g.Expect(name).To(gomega.HavePrefix("my-config-"))
		g.Expect(name).To(gomega.HaveLen(len("my-config-") + HashSuffixLength))
	})

	t.Run("consistent names for same content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name1 := BuildConfigMapName("config", "content")
		name2 := BuildConfigMapName("config", "content")
		g.Expect(name1).To(gomega.Equal(name2))
	})

	t.Run("different names for different content", func(t *testing.T) {
		g := gomega.NewWithT(t)
		name1 := BuildConfigMapName("config", "content1")
		name2 := BuildConfigMapName("config", "content2")
		g.Expect(name1).NotTo(gomega.Equal(name2))
	})
}

func TestBuild(t *testing.T) {
	t.Run("names the ConfigMap after the content hash", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := Build("dex", "konflux-ui", "config.yaml", "content")
		g.Expect(cm.Name).To(gomega.Equal("dex-" + GenerateHashSuffix("content")))
		g.Expect(cm.Namespace).To(gomega.Equal("konflux-ui"))
		g.Expect(cm.Data).To(gomega.HaveKeyWithValue("config.yaml", "content"))
	})

	t.Run("sets TypeMeta so the object is ready for server-side apply", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := Build("dex", "konflux-ui", "config.yaml", "content")
		g.Expect(cm.APIVersion).To(gomega.Equal("v1"))
		g.Expect(cm.Kind).To(gomega.Equal("ConfigMap"))
	})

	t.Run("same content yields the same name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(Build("dex", "ns", "k", "same").Name).
			To(gomega.Equal(Build("dex", "ns", "k", "same").Name))
	})

	t.Run("changed content yields a different name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(Build("dex", "ns", "k", "one").Name).
			NotTo(gomega.Equal(Build("dex", "ns", "k", "two").Name))
	})
}
