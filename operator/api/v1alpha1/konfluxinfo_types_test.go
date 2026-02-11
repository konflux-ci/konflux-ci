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

package v1alpha1

import (
	"maps"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

func TestClusterConfigData_All(t *testing.T) {
	t.Run("should yield all non-empty fields", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			DefaultOIDCIssuer:         "https://oidc.example.com",
			EnableKeylessSigning:      ptr.To(true),
			FulcioInternalUrl:         "https://fulcio-internal.example.com",
			FulcioExternalUrl:         "https://fulcio-external.example.com",
			RekorInternalUrl:          "https://rekor-internal.example.com",
			RekorExternalUrl:          "https://rekor-external.example.com",
			TufInternalUrl:            "https://tuf-internal.example.com",
			TufExternalUrl:            "https://tuf-external.example.com",
			TrustifyServerInternalUrl: "https://trustify-internal.example.com",
			TrustifyServerExternalUrl: "https://trustify-external.example.com",
		}

		collected := maps.Collect(data.All)

		g.Expect(collected).To(gomega.HaveLen(10))
		g.Expect(collected["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(collected["enableKeylessSigning"]).To(gomega.Equal("true"))
		g.Expect(collected["fulcioInternalUrl"]).To(gomega.Equal("https://fulcio-internal.example.com"))
		g.Expect(collected["fulcioExternalUrl"]).To(gomega.Equal("https://fulcio-external.example.com"))
		g.Expect(collected["rekorInternalUrl"]).To(gomega.Equal("https://rekor-internal.example.com"))
		g.Expect(collected["rekorExternalUrl"]).To(gomega.Equal("https://rekor-external.example.com"))
		g.Expect(collected["tufInternalUrl"]).To(gomega.Equal("https://tuf-internal.example.com"))
		g.Expect(collected["tufExternalUrl"]).To(gomega.Equal("https://tuf-external.example.com"))
		g.Expect(collected["trustifyServerInternalUrl"]).To(gomega.Equal("https://trustify-internal.example.com"))
		g.Expect(collected["trustifyServerExternalUrl"]).To(gomega.Equal("https://trustify-external.example.com"))
	})

	t.Run("should not yield empty fields", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			DefaultOIDCIssuer: "https://oidc.example.com",
			// All other fields are empty/nil
		}

		collected := maps.Collect(data.All)

		g.Expect(collected).To(gomega.HaveLen(1))
		g.Expect(collected["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(collected).NotTo(gomega.HaveKey("enableKeylessSigning"))
		g.Expect(collected).NotTo(gomega.HaveKey("fulcioInternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("fulcioExternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("rekorInternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("rekorExternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("tufInternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("tufExternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("trustifyServerInternalUrl"))
		g.Expect(collected).NotTo(gomega.HaveKey("trustifyServerExternalUrl"))
	})

	t.Run("should yield nothing for empty struct", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{}

		collected := maps.Collect(data.All)

		g.Expect(collected).To(gomega.BeEmpty())
	})

	t.Run("should support early termination", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			DefaultOIDCIssuer:         "https://oidc.example.com",
			EnableKeylessSigning:      ptr.To(true),
			FulcioInternalUrl:         "https://fulcio-internal.example.com",
			FulcioExternalUrl:         "https://fulcio-external.example.com",
			RekorInternalUrl:          "https://rekor-internal.example.com",
			RekorExternalUrl:          "https://rekor-external.example.com",
			TufInternalUrl:            "https://tuf-internal.example.com",
			TufExternalUrl:            "https://tuf-external.example.com",
			TrustifyServerInternalUrl: "https://trustify-internal.example.com",
			TrustifyServerExternalUrl: "https://trustify-external.example.com",
		}

		var yielded []string
		data.All(func(key, value string) bool {
			yielded = append(yielded, key)
			// Stop after first yield
			return false
		})

		g.Expect(yielded).To(gomega.HaveLen(1))
		g.Expect(yielded[0]).To(gomega.Equal("defaultOIDCIssuer"))
	})

	t.Run("should yield fields in correct order", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			DefaultOIDCIssuer:         "oidc",
			EnableKeylessSigning:      ptr.To(false),
			FulcioInternalUrl:         "fulcio-internal",
			FulcioExternalUrl:         "fulcio-external",
			RekorInternalUrl:          "rekor-internal",
			RekorExternalUrl:          "rekor-external",
			TufInternalUrl:            "tuf-internal",
			TufExternalUrl:            "tuf-external",
			TrustifyServerInternalUrl: "trustify-internal",
			TrustifyServerExternalUrl: "trustify-external",
		}

		var keys []string
		data.All(func(key, value string) bool {
			keys = append(keys, key)
			return true
		})

		expectedOrder := []string{
			"defaultOIDCIssuer",
			"enableKeylessSigning",
			"fulcioInternalUrl",
			"fulcioExternalUrl",
			"rekorInternalUrl",
			"rekorExternalUrl",
			"tufInternalUrl",
			"tufExternalUrl",
			"trustifyServerInternalUrl",
			"trustifyServerExternalUrl",
		}

		g.Expect(keys).To(gomega.Equal(expectedOrder))
	})

	t.Run("should yield enableKeylessSigning false as string", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			EnableKeylessSigning: ptr.To(false),
		}

		collected := maps.Collect(data.All)

		g.Expect(collected).To(gomega.HaveLen(1))
		g.Expect(collected["enableKeylessSigning"]).To(gomega.Equal("false"))
	})

	t.Run("should yield enableKeylessSigning true as string", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			EnableKeylessSigning: ptr.To(true),
		}

		collected := maps.Collect(data.All)

		g.Expect(collected).To(gomega.HaveLen(1))
		g.Expect(collected["enableKeylessSigning"]).To(gomega.Equal("true"))
	})

	t.Run("should not yield enableKeylessSigning when nil", func(t *testing.T) {
		g := gomega.NewWithT(t)

		data := ClusterConfigData{
			EnableKeylessSigning: nil,
		}

		collected := maps.Collect(data.All)

		g.Expect(collected).NotTo(gomega.HaveKey("enableKeylessSigning"))
	})
}

func TestClusterConfigData_MergeOver(t *testing.T) {
	t.Run("should merge base and override values", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{
			DefaultOIDCIssuer: "https://base-oidc.example.com",
			FulcioInternalUrl: "https://base-fulcio-internal.example.com",
		}

		override := ClusterConfigData{
			DefaultOIDCIssuer: "https://override-oidc.example.com",
			RekorExternalUrl:  "https://override-rekor-external.example.com",
		}

		result := override.MergeOver(base)

		// Override values should take precedence
		g.Expect(result["defaultOIDCIssuer"]).To(gomega.Equal("https://override-oidc.example.com"))
		// Base values should be included if not overridden
		g.Expect(result["fulcioInternalUrl"]).To(gomega.Equal("https://base-fulcio-internal.example.com"))
		// Override-only values should be included
		g.Expect(result["rekorExternalUrl"]).To(gomega.Equal("https://override-rekor-external.example.com"))
		g.Expect(result).To(gomega.HaveLen(3))
	})

	t.Run("should handle empty base", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{}

		override := ClusterConfigData{
			DefaultOIDCIssuer: "https://oidc.example.com",
			FulcioInternalUrl: "https://fulcio-internal.example.com",
		}

		result := override.MergeOver(base)

		g.Expect(result).To(gomega.HaveLen(2))
		g.Expect(result["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(result["fulcioInternalUrl"]).To(gomega.Equal("https://fulcio-internal.example.com"))
	})

	t.Run("should handle empty override", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{
			DefaultOIDCIssuer: "https://oidc.example.com",
			FulcioInternalUrl: "https://fulcio-internal.example.com",
		}

		override := ClusterConfigData{}

		result := override.MergeOver(base)

		g.Expect(result).To(gomega.HaveLen(2))
		g.Expect(result["defaultOIDCIssuer"]).To(gomega.Equal("https://oidc.example.com"))
		g.Expect(result["fulcioInternalUrl"]).To(gomega.Equal("https://fulcio-internal.example.com"))
	})

	t.Run("should handle both empty", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{}
		override := ClusterConfigData{}

		result := override.MergeOver(base)

		g.Expect(result).To(gomega.BeEmpty())
	})

	t.Run("should override all base values when override has all fields", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{
			DefaultOIDCIssuer:         "base-oidc",
			EnableKeylessSigning:      ptr.To(false),
			FulcioInternalUrl:         "base-fulcio-internal",
			FulcioExternalUrl:         "base-fulcio-external",
			RekorInternalUrl:          "base-rekor-internal",
			RekorExternalUrl:          "base-rekor-external",
			TufInternalUrl:            "base-tuf-internal",
			TufExternalUrl:            "base-tuf-external",
			TrustifyServerInternalUrl: "base-trustify-internal",
			TrustifyServerExternalUrl: "base-trustify-external",
		}

		override := ClusterConfigData{
			DefaultOIDCIssuer:         "override-oidc",
			EnableKeylessSigning:      ptr.To(true),
			FulcioInternalUrl:         "override-fulcio-internal",
			FulcioExternalUrl:         "override-fulcio-external",
			RekorInternalUrl:          "override-rekor-internal",
			RekorExternalUrl:          "override-rekor-external",
			TufInternalUrl:            "override-tuf-internal",
			TufExternalUrl:            "override-tuf-external",
			TrustifyServerInternalUrl: "override-trustify-internal",
			TrustifyServerExternalUrl: "override-trustify-external",
		}

		result := override.MergeOver(base)

		g.Expect(result).To(gomega.HaveLen(10))
		g.Expect(result["defaultOIDCIssuer"]).To(gomega.Equal("override-oidc"))
		g.Expect(result["enableKeylessSigning"]).To(gomega.Equal("true"))
		g.Expect(result["fulcioInternalUrl"]).To(gomega.Equal("override-fulcio-internal"))
		g.Expect(result["fulcioExternalUrl"]).To(gomega.Equal("override-fulcio-external"))
		g.Expect(result["rekorInternalUrl"]).To(gomega.Equal("override-rekor-internal"))
		g.Expect(result["rekorExternalUrl"]).To(gomega.Equal("override-rekor-external"))
		g.Expect(result["tufInternalUrl"]).To(gomega.Equal("override-tuf-internal"))
		g.Expect(result["tufExternalUrl"]).To(gomega.Equal("override-tuf-external"))
		g.Expect(result["trustifyServerInternalUrl"]).To(gomega.Equal("override-trustify-internal"))
		g.Expect(result["trustifyServerExternalUrl"]).To(gomega.Equal("override-trustify-external"))
	})

	t.Run("should combine base and override when no conflicts", func(t *testing.T) {
		g := gomega.NewWithT(t)

		base := ClusterConfigData{
			DefaultOIDCIssuer: "https://base-oidc.example.com",
			FulcioInternalUrl: "https://base-fulcio-internal.example.com",
		}

		override := ClusterConfigData{
			RekorExternalUrl: "https://override-rekor-external.example.com",
			TufExternalUrl:   "https://override-tuf-external.example.com",
		}

		result := override.MergeOver(base)

		g.Expect(result).To(gomega.HaveLen(4))
		g.Expect(result["defaultOIDCIssuer"]).To(gomega.Equal("https://base-oidc.example.com"))
		g.Expect(result["fulcioInternalUrl"]).To(gomega.Equal("https://base-fulcio-internal.example.com"))
		g.Expect(result["rekorExternalUrl"]).To(gomega.Equal("https://override-rekor-external.example.com"))
		g.Expect(result["tufExternalUrl"]).To(gomega.Equal("https://override-tuf-external.example.com"))
	})
}
