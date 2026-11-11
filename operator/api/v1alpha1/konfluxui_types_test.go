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
	"regexp"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	"k8s.io/utils/ptr"
)

func TestRuntimeConfigSpec_All(t *testing.T) {
	t.Run("should yield all fields when fully populated", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{
			ChatBot: &ChatBotConfig{
				Enabled: ptr.To(true),
			},
			Monitoring: &MonitoringConfig{
				Enabled:          ptr.To(true),
				DSN:              "https://sentry.example.com/123",
				Environment:      "production",
				Cluster:          "cluster-01",
				SampleRateErrors: "0.5",
			},
		}

		collected := maps.Collect(rc.All)

		g.Expect(collected).To(gomega.HaveLen(6))
		g.Expect(collected).To(gomega.Equal(map[string]string{
			"RUNTIME_CHAT_BOT_ENABLED":              "true",
			"RUNTIME_MONITORING_ENABLED":            "true",
			"RUNTIME_MONITORING_DSN":                "https://sentry.example.com/123",
			"RUNTIME_MONITORING_ENVIRONMENT":        "production",
			"RUNTIME_MONITORING_CLUSTER":            "cluster-01",
			"RUNTIME_MONITORING_SAMPLE_RATE_ERRORS": "0.5",
		}))
	})

	t.Run("should yield nothing for empty struct", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{}

		collected := maps.Collect(rc.All)

		g.Expect(collected).To(gomega.BeEmpty())
	})

	t.Run("should not yield empty or nil fields", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{
			ChatBot: &ChatBotConfig{},
			Monitoring: &MonitoringConfig{
				DSN: "https://sentry.example.com/123",
			},
		}

		collected := maps.Collect(rc.All)

		g.Expect(collected).To(gomega.HaveLen(1))
		g.Expect(collected["RUNTIME_MONITORING_DSN"]).To(gomega.Equal("https://sentry.example.com/123"))
		g.Expect(collected).NotTo(gomega.HaveKey("RUNTIME_CHAT_BOT_ENABLED"))
		g.Expect(collected).NotTo(gomega.HaveKey("RUNTIME_MONITORING_ENABLED"))
		g.Expect(collected).NotTo(gomega.HaveKey("RUNTIME_MONITORING_ENVIRONMENT"))
		g.Expect(collected).NotTo(gomega.HaveKey("RUNTIME_MONITORING_CLUSTER"))
		g.Expect(collected).NotTo(gomega.HaveKey("RUNTIME_MONITORING_SAMPLE_RATE_ERRORS"))
	})

	t.Run("should yield only chatbot when monitoring is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{
			ChatBot: &ChatBotConfig{
				Enabled: ptr.To(false),
			},
		}

		collected := maps.Collect(rc.All)

		g.Expect(collected).To(gomega.HaveLen(1))
		g.Expect(collected["RUNTIME_CHAT_BOT_ENABLED"]).To(gomega.Equal("false"))
	})

	t.Run("should yield only monitoring when chatbot is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{
			Monitoring: &MonitoringConfig{
				Enabled:     ptr.To(false),
				Environment: "staging",
			},
		}

		collected := maps.Collect(rc.All)

		g.Expect(collected).To(gomega.HaveLen(2))
		g.Expect(collected["RUNTIME_MONITORING_ENABLED"]).To(gomega.Equal("false"))
		g.Expect(collected["RUNTIME_MONITORING_ENVIRONMENT"]).To(gomega.Equal("staging"))
	})

	t.Run("should support early termination", func(t *testing.T) {
		g := gomega.NewWithT(t)

		rc := RuntimeConfigSpec{
			ChatBot: &ChatBotConfig{
				Enabled: ptr.To(true),
			},
			Monitoring: &MonitoringConfig{
				Enabled:     ptr.To(true),
				DSN:         "https://sentry.example.com/123",
				Environment: "production",
			},
		}

		var count int
		rc.All(func(_, _ string) bool {
			count++
			return count < 2
		})

		g.Expect(count).To(gomega.Equal(2))
	})
}

func TestKonfluxUIConfigSpec_Accessors(t *testing.T) {
	t.Parallel()
	g := gomega.NewWithT(t)

	cfg := &KonfluxUIConfigSpec{}

	g.Expect(cfg.GetIngress()).To(gomega.Equal(IngressSpec{}))
	g.Expect(cfg.GetNodePortService()).To(gomega.BeNil())
	g.Expect(cfg.GetProxy()).To(gomega.Equal(ProxyDeploymentSpec{Replicas: 1}))
	g.Expect(cfg.GetDex()).To(gomega.Equal(DexDeploymentSpec{Replicas: 1}))
}

// Patterns must stay in sync with +kubebuilder:validation:Pattern markers.
var (
	// ingressFQDNPattern mirrors the Pattern on IngressSpec.FQDN and AdditionalFQDNs.
	ingressFQDNPattern = regexp.MustCompile(
		`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?` +
			`(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*` +
			`\.?(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`,
	)
	ingressHostnamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	// endpointHostnameWithPortPattern mirrors the Pattern on EndpointSpec.Hostname
	// and WatsonEndpointSpec.Hostname (port allowed).
	endpointHostnameWithPortPattern = regexp.MustCompile(
		`^$|^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?` +
			`(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*` +
			`\.?(:([1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5]))?$`,
	)
	// endpointHostnameNoPortPattern mirrors the Pattern on TektonResultsSpec.Hostname
	// (no port — the Caddy template appends :8080).
	endpointHostnameNoPortPattern = regexp.MustCompile(
		`^$|^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?` +
			`(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*\.?$`,
	)
)

func TestIngressSpecValidationPatterns(t *testing.T) {
	t.Parallel()

	t.Run("fqdn accepts host and optional port", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"konflux.example.com",
			"host.example.com.",
			"ui.example.com:8443",
			"host:8443",
			"localhost",
			"a",
		} {
			g.Expect(ingressFQDNPattern.MatchString(value)).To(gomega.BeTrue(), "fqdn %q", value)
		}
	})

	t.Run("fqdn rejects empty and invalid values", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"",
			"foo..bar",
			"-foo.bar",
			"foo-.bar",
			"a" + strings.Repeat("b", 63),
			"host:0",
			"host:99999",
			"host:65536",
			"-bad.example.com",
			"example.com:",
			"example.com:port",
		} {
			g.Expect(ingressFQDNPattern.MatchString(value)).To(gomega.BeFalse(), "fqdn %q", value)
		}
	})

	t.Run("hostname accepts a single DNS label", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"konflux-ui",
			"a",
			"ui1",
		} {
			g.Expect(ingressHostnamePattern.MatchString(value)).To(gomega.BeTrue(), "hostname %q", value)
		}
	})

	t.Run("hostname rejects dotted or uppercase names", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"bad.dotted.name",
			"Konflux-UI",
			"-leading",
			"trailing-",
			"",
		} {
			g.Expect(ingressHostnamePattern.MatchString(value)).To(gomega.BeFalse(), "hostname %q", value)
		}
	})
}

func TestEndpointHostnameValidationPatterns(t *testing.T) {
	t.Parallel()

	t.Run("with-port pattern accepts empty, DNS names, trailing dot, and port", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"",
			"host.example.com",
			"host.example.com.",
			"host:8443",
			"kite.example.svc:8443",
			"a",
		} {
			g.Expect(endpointHostnameWithPortPattern.MatchString(value)).To(gomega.BeTrue(), "endpoint %q", value)
		}
	})

	t.Run("with-port pattern rejects invalid hostnames and ports", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"foo..bar",
			"-foo.bar",
			"foo-.bar",
			"a" + strings.Repeat("b", 63),
			"host:0",
			"host:99999",
			"host:65536",
		} {
			g.Expect(endpointHostnameWithPortPattern.MatchString(value)).To(gomega.BeFalse(), "endpoint %q", value)
		}
	})

	t.Run("no-port pattern accepts empty, DNS names, and trailing dot", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"",
			"host.example.com",
			"host.example.com.",
			"a",
			"tekton-results-api-service.tekton-pipelines.svc.cluster.local",
		} {
			g.Expect(endpointHostnameNoPortPattern.MatchString(value)).To(gomega.BeTrue(), "tekton-results %q", value)
		}
	})

	t.Run("no-port pattern rejects ports and invalid hostnames", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"host:8443",
			"foo..bar",
			"-foo.bar",
			"foo-.bar",
			"a" + strings.Repeat("b", 63),
		} {
			g.Expect(endpointHostnameNoPortPattern.MatchString(value)).To(gomega.BeFalse(), "tekton-results %q", value)
		}
	})
}
