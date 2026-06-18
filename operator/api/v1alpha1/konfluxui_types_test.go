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
	"net/url"
	"testing"

	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	"github.com/onsi/gomega"
)

func TestResolveDexEndpoint(t *testing.T) {
	defaultEndpoint := &url.URL{Scheme: "https", Host: "konflux.example.com"}

	t.Run("returns default endpoint when no dex config is set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ui := &KonfluxUI{}

		result := ui.ResolveDexEndpoint(defaultEndpoint)

		g.Expect(result).To(gomega.Equal(defaultEndpoint))
	})

	t.Run("returns default endpoint when dex config has no hostname", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ui := &KonfluxUI{
			Spec: KonfluxUISpec{
				Dex: &DexDeploymentSpec{
					Config: &dex.DexParams{},
				},
			},
		}

		result := ui.ResolveDexEndpoint(defaultEndpoint)

		g.Expect(result).To(gomega.Equal(defaultEndpoint))
	})

	t.Run("returns overridden endpoint when hostname is set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ui := &KonfluxUI{
			Spec: KonfluxUISpec{
				Dex: &DexDeploymentSpec{
					Config: &dex.DexParams{
						Hostname: "custom.example.com",
					},
				},
			},
		}

		result := ui.ResolveDexEndpoint(defaultEndpoint)

		g.Expect(result.Scheme).To(gomega.Equal("https"))
		g.Expect(result.Host).To(gomega.Equal("custom.example.com"))
		g.Expect(result.String()).To(gomega.Equal("https://custom.example.com"))
	})

	t.Run("returns overridden endpoint when hostname and port are set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		ui := &KonfluxUI{
			Spec: KonfluxUISpec{
				Dex: &DexDeploymentSpec{
					Config: &dex.DexParams{
						Hostname: "custom.example.com",
						Port:     "8443",
					},
				},
			},
		}

		result := ui.ResolveDexEndpoint(defaultEndpoint)

		g.Expect(result.Scheme).To(gomega.Equal("https"))
		g.Expect(result.Host).To(gomega.Equal("custom.example.com:8443"))
		g.Expect(result.String()).To(gomega.Equal("https://custom.example.com:8443"))
	})

	t.Run("does not modify the default endpoint", func(t *testing.T) {
		g := gomega.NewWithT(t)
		original := &url.URL{Scheme: "https", Host: "original.example.com"}
		ui := &KonfluxUI{
			Spec: KonfluxUISpec{
				Dex: &DexDeploymentSpec{
					Config: &dex.DexParams{
						Hostname: "custom.example.com",
					},
				},
			},
		}

		_ = ui.ResolveDexEndpoint(original)

		g.Expect(original.Host).To(gomega.Equal("original.example.com"))
	})
}
