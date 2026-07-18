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
	"encoding/json"
	"reflect"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

// TestKonfluxParentReleaseAndUINestedComponentMetricsExcluded verifies that
// the parent Konflux CR does not expose componentMetrics under
// releaseService.spec or ui.spec.
func TestKonfluxParentReleaseAndUINestedComponentMetricsExcluded(t *testing.T) {
	t.Parallel()

	t.Run("ReleaseServiceConfig.Spec type does not have ComponentMetrics field", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		typ := reflect.TypeOf(KonfluxReleaseServiceConfigSpec{})
		_, found := typ.FieldByName("ComponentMetrics")
		g.Expect(found).To(gomega.BeFalse(),
			"KonfluxReleaseServiceConfigSpec must not contain ComponentMetrics")
	})

	t.Run("KonfluxUIConfig.Spec type does not have ComponentMetrics field", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		typ := reflect.TypeOf(KonfluxUIConfigSpec{})
		_, found := typ.FieldByName("ComponentMetrics")
		g.Expect(found).To(gomega.BeFalse(),
			"KonfluxUIConfigSpec must not contain ComponentMetrics")
	})

	t.Run("nested componentMetrics in parent JSON is ignored for release", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		parentJSON := `{
			"apiVersion": "konflux.konflux-ci.dev/v1alpha1",
			"kind": "Konflux",
			"metadata": {"name": "konflux"},
			"spec": {
				"componentMetrics": {"enabled": true},
				"releaseService": {
					"spec": {
						"debug": true,
						"componentMetrics": {"enabled": false}
					}
				}
			}
		}`

		var konflux Konflux
		err := json.Unmarshal([]byte(parentJSON), &konflux)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// The top-level componentMetrics should be parsed
		g.Expect(konflux.Spec.ComponentMetrics).NotTo(gomega.BeNil())
		g.Expect(konflux.Spec.ComponentMetrics.IsEnabled()).To(gomega.BeTrue())

		// The nested componentMetrics under releaseService.spec should be dropped
		// because KonfluxReleaseServiceConfigSpec does not have that field
		g.Expect(konflux.Spec.KonfluxReleaseService).NotTo(gomega.BeNil())
		g.Expect(konflux.Spec.KonfluxReleaseService.Spec).NotTo(gomega.BeNil())
		g.Expect(konflux.Spec.KonfluxReleaseService.Spec.Debug).To(gomega.BeTrue())
	})

	t.Run("nested componentMetrics in parent JSON is ignored for UI", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		parentJSON := `{
			"apiVersion": "konflux.konflux-ci.dev/v1alpha1",
			"kind": "Konflux",
			"metadata": {"name": "konflux"},
			"spec": {
				"componentMetrics": {"enabled": true},
				"ui": {
					"spec": {
						"componentMetrics": {"enabled": false}
					}
				}
			}
		}`

		var konflux Konflux
		err := json.Unmarshal([]byte(parentJSON), &konflux)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		// The top-level componentMetrics should be parsed
		g.Expect(konflux.Spec.ComponentMetrics).NotTo(gomega.BeNil())
		g.Expect(konflux.Spec.ComponentMetrics.IsEnabled()).To(gomega.BeTrue())

		// The nested componentMetrics under ui.spec should be dropped
		// because KonfluxUIConfigSpec does not have that field
		g.Expect(konflux.Spec.KonfluxUI).NotTo(gomega.BeNil())
		g.Expect(konflux.Spec.KonfluxUI.Spec).NotTo(gomega.BeNil())
	})

	t.Run("constructors correctly compose ConfigSpec with forwarded metrics", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		metrics := &ComponentMetricsConfig{Enabled: ptr.To(true)}

		// Release service
		relCfg := KonfluxReleaseServiceConfigSpec{Debug: true}
		relSpec := NewKonfluxReleaseServiceSpec(relCfg, metrics)
		g.Expect(relSpec.Debug).To(gomega.BeTrue())
		g.Expect(relSpec.ComponentMetrics).To(gomega.Equal(metrics))

		// UI
		uiCfg := KonfluxUIConfigSpec{
			Ingress: &IngressSpec{Host: "test.example.com"},
		}
		uiSpec := NewKonfluxUISpec(uiCfg, metrics)
		g.Expect(uiSpec.Ingress.Host).To(gomega.Equal("test.example.com"))
		g.Expect(uiSpec.ComponentMetrics).To(gomega.Equal(metrics))
	})

	t.Run("omitting componentMetrics propagates default behavior", func(t *testing.T) {
		t.Parallel()
		g := gomega.NewWithT(t)

		// When no componentMetrics is specified, the default is enabled
		relSpec := NewKonfluxReleaseServiceSpec(KonfluxReleaseServiceConfigSpec{}, nil)
		g.Expect(relSpec.ComponentMetrics).To(gomega.BeNil())
		// ComponentMetricsConfig.IsEnabled() returns true when nil
		g.Expect(relSpec.ComponentMetrics.IsEnabled()).To(gomega.BeTrue())

		uiSpec := NewKonfluxUISpec(KonfluxUIConfigSpec{}, nil)
		g.Expect(uiSpec.ComponentMetrics).To(gomega.BeNil())
		g.Expect(uiSpec.ComponentMetrics.IsEnabled()).To(gomega.BeTrue())
	})
}
