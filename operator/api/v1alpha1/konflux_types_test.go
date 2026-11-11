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

// TestKonfluxSpec_StandardDefaults verifies that all component enabled helpers
// return the expected values for the default (standard) installation type.
func TestKonfluxSpec_StandardDefaults(t *testing.T) {
	g := gomega.NewWithT(t)
	spec := &KonfluxSpec{}

	g.Expect(spec.GetInstallationType()).To(gomega.Equal(InstallationTypeStandard),
		"empty installationType defaults to standard")

	// Standard-type components: enabled in standard mode.
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeTrue(), "applicationAPI enabled in standard")
	g.Expect(spec.IsUIEnabled()).To(gomega.BeTrue(), "ui enabled in standard")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeTrue(), "integrationService enabled in standard")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeTrue(), "releaseService enabled in standard")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeTrue(), "buildService enabled in standard")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeTrue(), "namespaceLister enabled in standard")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeTrue(), "enterpriseContract enabled in standard")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeTrue(), "certManager enabled in standard")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeTrue(), "cli enabled in standard")
	g.Expect(spec.IsDefaultTenantEnabled()).To(gomega.BeTrue(), "defaultTenant enabled in standard")

	// Both-type components: enabled in standard mode.
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info enabled in standard")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac enabled in standard")

	// Optional components: disabled by default regardless of type.
	g.Expect(spec.IsImageControllerEnabled()).To(gomega.BeFalse(), "imageController defaults to disabled")
	g.Expect(spec.IsInternalRegistryEnabled()).To(gomega.BeFalse(), "internalRegistry defaults to disabled")
	g.Expect(spec.IsTelemetryEnabled()).To(gomega.BeFalse(), "telemetry defaults to disabled")
}

// TestKonfluxSpec_ExplicitStandard verifies that explicitly setting
// InstallationTypeStandard behaves the same as the empty-string default.
func TestKonfluxSpec_ExplicitStandard(t *testing.T) {
	g := gomega.NewWithT(t)
	spec := &KonfluxSpec{InstallationType: InstallationTypeStandard}

	g.Expect(spec.GetInstallationType()).To(gomega.Equal(InstallationTypeStandard))
	g.Expect(spec.IsStandard()).To(gomega.BeTrue())

	// Standard-type components: enabled.
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeTrue(), "applicationAPI enabled")
	g.Expect(spec.IsUIEnabled()).To(gomega.BeTrue(), "ui enabled")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeTrue(), "integrationService enabled")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeTrue(), "releaseService enabled")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeTrue(), "buildService enabled")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeTrue(), "namespaceLister enabled")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeTrue(), "enterpriseContract enabled")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeTrue(), "certManager enabled")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeTrue(), "cli enabled")
	g.Expect(spec.IsDefaultTenantEnabled()).To(gomega.BeTrue(), "defaultTenant enabled")

	// Both-type components: enabled.
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info enabled")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac enabled")
}

// TestKonfluxSpec_SpokeType verifies that the spoke installation type enables
// only Info and RBAC, disabling all standard-only components.
func TestKonfluxSpec_SpokeType(t *testing.T) {
	g := gomega.NewWithT(t)
	spec := &KonfluxSpec{InstallationType: InstallationTypeSpoke}

	g.Expect(spec.GetInstallationType()).To(gomega.Equal(InstallationTypeSpoke))

	// Standard-only components: disabled in spoke mode.
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeFalse(), "applicationAPI disabled in spoke")
	g.Expect(spec.IsUIEnabled()).To(gomega.BeFalse(), "ui disabled in spoke")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeFalse(), "integrationService disabled in spoke")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeFalse(), "releaseService disabled in spoke")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeFalse(), "buildService disabled in spoke")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeFalse(), "namespaceLister disabled in spoke")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeFalse(), "enterpriseContract disabled in spoke")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeFalse(), "certManager disabled in spoke")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeFalse(), "cli disabled in spoke")
	g.Expect(spec.IsDefaultTenantEnabled()).To(gomega.BeFalse(), "defaultTenant disabled in spoke")

	// Both-type components: enabled in spoke mode.
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info enabled in spoke")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac enabled in spoke")

	// Optional components: still disabled by default.
	g.Expect(spec.IsImageControllerEnabled()).To(gomega.BeFalse(), "imageController defaults to disabled in spoke")
	g.Expect(spec.IsInternalRegistryEnabled()).To(gomega.BeFalse(), "internalRegistry defaults to disabled in spoke")
	g.Expect(spec.IsTelemetryEnabled()).To(gomega.BeFalse(), "telemetry defaults to disabled in spoke")
}

// TestKonfluxSpec_SpokeWithOptionalComponents verifies that optional components
// remain independently toggleable in spoke mode.
func TestKonfluxSpec_SpokeWithOptionalComponents(t *testing.T) {
	g := gomega.NewWithT(t)
	enabled := true
	spec := &KonfluxSpec{
		InstallationType: InstallationTypeSpoke,
		ImageController:  &ImageControllerConfig{Enabled: &enabled},
		InternalRegistry: &InternalRegistryConfig{Enabled: &enabled},
		Telemetry:        &TelemetryConfig{Enabled: &enabled},
	}

	// Optional components can be enabled even in spoke mode.
	g.Expect(spec.IsImageControllerEnabled()).To(gomega.BeTrue(), "imageController enabled in spoke when set")
	g.Expect(spec.IsInternalRegistryEnabled()).To(gomega.BeTrue(), "internalRegistry enabled in spoke when set")
	g.Expect(spec.IsTelemetryEnabled()).To(gomega.BeTrue(), "telemetry enabled in spoke when set")

	// Standard-only components remain disabled even with deprecated enabled=true.
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeFalse(), "buildService still disabled in spoke")
}

// TestKonfluxSpec_DeprecatedEnabledFieldHonored verifies that the deprecated
// Enabled field on standard-only components is honored in standard mode.
// This ensures backward compatibility for existing CRs that set enabled: false.
func TestKonfluxSpec_DeprecatedEnabledFieldHonored(t *testing.T) {
	g := gomega.NewWithT(t)
	disabled := false

	spec := &KonfluxSpec{
		KonfluxUI:                 &KonfluxUIConfig{Enabled: &disabled},
		KonfluxIntegrationService: &IntegrationServiceConfig{Enabled: &disabled},
		KonfluxReleaseService:     &ReleaseServiceConfig{Enabled: &disabled},
		KonfluxBuildService:       &BuildServiceConfig{Enabled: &disabled},
		NamespaceLister:           &NamespaceListerConfig{Enabled: &disabled},
		EnterpriseContract:        &EnterpriseContractConfig{Enabled: &disabled},
		CertManager:               &CertManagerConfig{Enabled: &disabled},
		ApplicationAPI:            &ApplicationAPIConfig{Enabled: &disabled},
		CLI:                       &CLIConfig{Enabled: &disabled},
	}

	// All standard-only components disabled via deprecated field in standard mode.
	g.Expect(spec.IsUIEnabled()).To(gomega.BeFalse(), "ui disabled via deprecated enabled=false")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeFalse(), "integrationService disabled via deprecated enabled=false")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeFalse(), "releaseService disabled via deprecated enabled=false")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeFalse(), "buildService disabled via deprecated enabled=false")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeFalse(), "namespaceLister disabled via deprecated enabled=false")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeFalse(), "enterpriseContract disabled via deprecated enabled=false")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeFalse(), "certManager disabled via deprecated enabled=false")
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeFalse(), "applicationAPI disabled via deprecated enabled=false")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeFalse(), "cli disabled via deprecated enabled=false")

	// Always-on components (Info, RBAC) ignore the deprecated Enabled field.
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info always enabled regardless of deprecated field")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac always enabled regardless of deprecated field")
}

// TestKonfluxSpec_DeprecatedEnabledFieldIgnoredInSpoke verifies that the
// deprecated Enabled field cannot override spoke mode — standard-only
// components remain disabled in spoke mode even with enabled: true.
func TestKonfluxSpec_DeprecatedEnabledFieldIgnoredInSpoke(t *testing.T) {
	g := gomega.NewWithT(t)
	enabled := true

	spec := &KonfluxSpec{
		InstallationType:          InstallationTypeSpoke,
		KonfluxUI:                 &KonfluxUIConfig{Enabled: &enabled},
		KonfluxIntegrationService: &IntegrationServiceConfig{Enabled: &enabled},
		KonfluxReleaseService:     &ReleaseServiceConfig{Enabled: &enabled},
		KonfluxBuildService:       &BuildServiceConfig{Enabled: &enabled},
		NamespaceLister:           &NamespaceListerConfig{Enabled: &enabled},
		EnterpriseContract:        &EnterpriseContractConfig{Enabled: &enabled},
		CertManager:               &CertManagerConfig{Enabled: &enabled},
		ApplicationAPI:            &ApplicationAPIConfig{Enabled: &enabled},
		CLI:                       &CLIConfig{Enabled: &enabled},
	}

	// All standard-only components disabled in spoke mode regardless of deprecated field.
	g.Expect(spec.IsUIEnabled()).To(gomega.BeFalse(), "ui disabled in spoke even with deprecated enabled=true")
	g.Expect(spec.IsIntegrationServiceEnabled()).To(gomega.BeFalse(), "integrationService disabled in spoke")
	g.Expect(spec.IsReleaseServiceEnabled()).To(gomega.BeFalse(), "releaseService disabled in spoke")
	g.Expect(spec.IsBuildServiceEnabled()).To(gomega.BeFalse(), "buildService disabled in spoke")
	g.Expect(spec.IsNamespaceListerEnabled()).To(gomega.BeFalse(), "namespaceLister disabled in spoke")
	g.Expect(spec.IsEnterpriseContractEnabled()).To(gomega.BeFalse(), "enterpriseContract disabled in spoke")
	g.Expect(spec.IsCertManagerEnabled()).To(gomega.BeFalse(), "certManager disabled in spoke")
	g.Expect(spec.IsApplicationAPIEnabled()).To(gomega.BeFalse(), "applicationAPI disabled in spoke")
	g.Expect(spec.IsCLIEnabled()).To(gomega.BeFalse(), "cli disabled in spoke")

	// Always-on components still enabled in spoke.
	g.Expect(spec.IsInfoEnabled()).To(gomega.BeTrue(), "info enabled in spoke")
	g.Expect(spec.IsRBACEnabled()).To(gomega.BeTrue(), "rbac enabled in spoke")
}

// TestKonfluxSpec_DefaultTenantEnabled verifies that the default tenant respects
// its Enabled field in standard mode and is always disabled in spoke mode.
func TestKonfluxSpec_DefaultTenantEnabled(t *testing.T) {
	g := gomega.NewWithT(t)

	// nil DefaultTenant config: enabled by default in standard.
	g.Expect((&KonfluxSpec{}).IsDefaultTenantEnabled()).To(gomega.BeTrue(),
		"nil DefaultTenant defaults to enabled in standard")

	// Explicitly enabled.
	enabled := true
	g.Expect((&KonfluxSpec{
		DefaultTenant: &DefaultTenantConfig{Enabled: &enabled},
	}).IsDefaultTenantEnabled()).To(gomega.BeTrue(),
		"explicit true keeps default tenant enabled")

	// Explicitly disabled in standard.
	disabled := false
	g.Expect((&KonfluxSpec{
		DefaultTenant: &DefaultTenantConfig{Enabled: &disabled},
	}).IsDefaultTenantEnabled()).To(gomega.BeFalse(),
		"explicit false disables default tenant in standard")

	// Spoke mode: disabled regardless of Enabled field.
	g.Expect((&KonfluxSpec{
		InstallationType: InstallationTypeSpoke,
		DefaultTenant:    &DefaultTenantConfig{Enabled: &enabled},
	}).IsDefaultTenantEnabled()).To(gomega.BeFalse(),
		"default tenant disabled in spoke even when explicitly enabled")
}
