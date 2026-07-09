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
	"k8s.io/utils/ptr"
)

func TestNewKonfluxBuildServiceSpec(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)

	cfg := KonfluxBuildServiceConfigSpec{
		LogEncoder: LogEncoderConsole,
	}
	metrics := &ComponentMetricsConfig{Enabled: ptr.To(false)}

	spec := NewKonfluxBuildServiceSpec(cfg, metrics)
	g.Expect(spec.LogEncoder).To(gomega.Equal(LogEncoderConsole))
	g.Expect(spec.ComponentMetrics).To(gomega.Equal(metrics))
	g.Expect(spec.KonfluxBuildServiceConfigSpec).To(gomega.Equal(cfg))
}

func TestNewKonfluxImageControllerSpec(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)

	cfg := KonfluxImageControllerConfigSpec{
		LogEncoder: LogEncoderJSON,
	}
	disabled := false
	metrics := &ComponentMetricsConfig{Enabled: &disabled}

	spec := NewKonfluxImageControllerSpec(cfg, metrics)
	g.Expect(spec.LogEncoder).To(gomega.Equal(LogEncoderJSON))
	g.Expect(spec.ComponentMetrics).To(gomega.Equal(metrics))
}

func TestNewKonfluxIntegrationServiceSpec(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)

	cfg := KonfluxIntegrationServiceConfigSpec{
		PipelineTimeout: "1h",
	}

	spec := NewKonfluxIntegrationServiceSpec(cfg, nil)
	g.Expect(spec.PipelineTimeout).To(gomega.Equal("1h"))
	g.Expect(spec.ComponentMetrics).To(gomega.BeNil())
}
