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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKonflux_IsReady(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&Konflux{}).IsReady()).To(gomega.BeFalse())

	k := &Konflux{
		Status: KonfluxStatus{
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
			},
		},
	}
	g.Expect(k.IsReady()).To(gomega.BeFalse())

	k.Status.Conditions[0].Status = metav1.ConditionTrue
	g.Expect(k.IsReady()).To(gomega.BeTrue())
}

func TestKonflux_ReadyConditionMessage(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&Konflux{}).ReadyConditionMessage()).
		To(gomega.Equal("konflux Ready condition not found in status"))

	k := &Konflux{
		Status: KonfluxStatus{
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "ComponentsNotReady",
					Message: "waiting for ui",
				},
			},
		},
	}
	g.Expect(k.ReadyConditionMessage()).
		To(gomega.Equal("konflux Ready=False reason=ComponentsNotReady message=waiting for ui"))
}

func TestComponentMetricsConfig_IsEnabled(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((*ComponentMetricsConfig)(nil).IsEnabled()).To(gomega.BeTrue())
	g.Expect((&ComponentMetricsConfig{}).IsEnabled()).To(gomega.BeTrue())

	disabled := false
	g.Expect((&ComponentMetricsConfig{Enabled: &disabled}).IsEnabled()).To(gomega.BeFalse())
}

func TestKonfluxSpec_IsComponentMetricsEnabled(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&KonfluxSpec{}).IsComponentMetricsEnabled()).To(gomega.BeTrue())

	unset := &KonfluxSpec{ComponentMetrics: &ComponentMetricsConfig{}}
	g.Expect(unset.IsComponentMetricsEnabled()).To(gomega.BeTrue())

	disabled := false
	g.Expect((&KonfluxSpec{
		ComponentMetrics: &ComponentMetricsConfig{Enabled: &disabled},
	}).IsComponentMetricsEnabled()).To(gomega.BeFalse())

	enabled := true
	g.Expect((&KonfluxSpec{
		ComponentMetrics: &ComponentMetricsConfig{Enabled: &enabled},
	}).IsComponentMetricsEnabled()).To(gomega.BeTrue())
}

// TestKonfluxSpec_EnabledDefaults verifies that all component enabled helpers
// return the expected defaults when the config block or enabled field is nil.
func TestKonfluxSpec_EnabledDefaults(t *testing.T) {
	g := gomega.NewWithT(t)
	spec := &KonfluxSpec{}

	// Components that default to true (enabled by default).
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeTrue(), "applicationAPI defaults to enabled")
	g.Expect(spec.IsUIEnabled()).To(gomega.BeTrue(), "ui defaults to enabled")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeTrue(), "integrationService defaults to enabled")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeTrue(), "releaseService defaults to enabled")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeTrue(), "buildService defaults to enabled")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeTrue(), "namespaceLister defaults to enabled")
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info defaults to enabled")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeTrue(), "enterpriseContract defaults to enabled")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeTrue(), "certManager defaults to enabled")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac defaults to enabled")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeTrue(), "cli defaults to enabled")
	g.Expect(spec.IsDefaultTenantEnabled()).To(gomega.BeTrue(), "defaultTenant defaults to enabled")

	// Components that default to false (disabled by default).
	g.Expect(spec.IsImageControllerEnabled()).To(gomega.BeFalse(), "imageController defaults to disabled")
	g.Expect(spec.IsInternalRegistryEnabled()).To(gomega.BeFalse(), "internalRegistry defaults to disabled")
	g.Expect(spec.IsTelemetryEnabled()).To(gomega.BeFalse(), "telemetry defaults to disabled")
}

// TestKonfluxSpec_EnabledExplicit verifies that setting enabled=false disables
// and enabled=true explicitly enables each component.
func TestKonfluxSpec_EnabledExplicit(t *testing.T) {
	g := gomega.NewWithT(t)
	disabled := false
	enabled := true

	// Build a spec with all explicitly set to the given value and verify each helper.
	buildSpec := func(val *bool) *KonfluxSpec {
		return &KonfluxSpec{
			ApplicationAPI:            &ApplicationAPIConfig{Enabled: val},
			KonfluxUI:                 &KonfluxUIConfig{Enabled: val},
			KonfluxIntegrationService: &IntegrationServiceConfig{Enabled: val},
			KonfluxReleaseService:     &ReleaseServiceConfig{Enabled: val},
			KonfluxBuildService:       &BuildServiceConfig{Enabled: val},
			NamespaceLister:           &NamespaceListerConfig{Enabled: val},
			KonfluxInfo:               &KonfluxInfoConfig{Enabled: val},
			EnterpriseContract:        &EnterpriseContractConfig{Enabled: val},
			CertManager:               &CertManagerConfig{Enabled: val},
			RBAC:                      &RBACConfig{Enabled: val},
			CLI:                       &CLIConfig{Enabled: val},
		}
	}

	// Helper returns all Is*Enabled results for the default-true components.
	results := func(s *KonfluxSpec) []bool {
		return []bool{
			s.IsApplicationAPIEnabled(),
			s.IsUIEnabled(),
			s.IsIntegrationServiceEnabled(),
			s.IsReleaseServiceEnabled(),
			s.IsBuildServiceEnabled(),
			s.IsNamespaceListerEnabled(),
			s.IsInfoEnabled(),
			s.IsEnterpriseContractEnabled(),
			s.IsCertManagerEnabled(),
			s.IsRBACEnabled(),
			s.IsCLIEnabled(),
		}
	}

	for _, v := range results(buildSpec(&disabled)) {
		g.Expect(v).To(gomega.BeFalse(), "all should be disabled when enabled=false")
	}
	for _, v := range results(buildSpec(&enabled)) {
		g.Expect(v).To(gomega.BeTrue(), "all should be enabled when enabled=true")
	}
}
