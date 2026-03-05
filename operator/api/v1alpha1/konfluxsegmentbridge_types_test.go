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
	"testing"

	"github.com/onsi/gomega"
)

func TestSanitizeSegmentHost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "clean URL is unchanged",
			input:    "https://console.redhat.com/connections/api/v1",
			expected: "https://console.redhat.com/connections/api/v1",
		},
		{
			name:     "trailing slash is stripped",
			input:    "https://console.redhat.com/connections/api/v1/",
			expected: "https://console.redhat.com/connections/api/v1",
		},
		{
			name:     "trailing /batch is stripped",
			input:    "https://console.redhat.com/connections/api/v1/batch",
			expected: "https://console.redhat.com/connections/api/v1",
		},
		{
			name:     "trailing /batch/ is stripped",
			input:    "https://console.redhat.com/connections/api/v1/batch/",
			expected: "https://console.redhat.com/connections/api/v1",
		},
		{
			name:     "default segment URL",
			input:    "https://api.segment.io/v1",
			expected: "https://api.segment.io/v1",
		},
		{
			name:     "default segment URL with /batch",
			input:    "https://api.segment.io/v1/batch",
			expected: "https://api.segment.io/v1",
		},
		{
			name:     "bare host without path",
			input:    "https://api.segment.io",
			expected: "https://api.segment.io",
		},
		{
			name:     "multiple trailing slashes",
			input:    "https://example.com/api///",
			expected: "https://example.com/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			g.Expect(sanitizeSegmentHost(tt.input)).To(gomega.Equal(tt.expected))
		})
	}
}

func TestGetSegmentAPIURL(t *testing.T) {
	t.Run("nil spec returns default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		var spec *KonfluxSegmentBridgeSpec
		g.Expect(spec.GetSegmentAPIURL()).To(gomega.Equal(DefaultSegmentAPIURL))
	})

	t.Run("empty SegmentAPIURL returns default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxSegmentBridgeSpec{}
		g.Expect(spec.GetSegmentAPIURL()).To(gomega.Equal(DefaultSegmentAPIURL))
	})

	t.Run("custom URL is returned sanitized", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxSegmentBridgeSpec{
			SegmentAPIURL: "https://console.redhat.com/connections/api/v1/",
		}
		g.Expect(spec.GetSegmentAPIURL()).To(gomega.Equal("https://console.redhat.com/connections/api/v1"))
	})

	t.Run("URL with accidental /batch is sanitized", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxSegmentBridgeSpec{
			SegmentAPIURL: "https://console.redhat.com/connections/api/v1/batch",
		}
		g.Expect(spec.GetSegmentAPIURL()).To(gomega.Equal("https://console.redhat.com/connections/api/v1"))
	})
}

func TestGetSegmentKey(t *testing.T) {
	t.Run("nil spec returns empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		var spec *KonfluxSegmentBridgeSpec
		g.Expect(spec.GetSegmentKey()).To(gomega.BeEmpty())
	})

	t.Run("empty key returns empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxSegmentBridgeSpec{}
		g.Expect(spec.GetSegmentKey()).To(gomega.BeEmpty())
	})

	t.Run("set key is returned", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxSegmentBridgeSpec{SegmentKey: "my-write-key"}
		g.Expect(spec.GetSegmentKey()).To(gomega.Equal("my-write-key"))
	})
}
