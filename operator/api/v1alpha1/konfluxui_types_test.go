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

// Patterns must stay in sync with +kubebuilder:validation:Pattern on IngressSpec.
var (
	ingressFQDNPattern     = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?(:[0-9]{1,5})?$`)
	ingressHostnamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

func TestIngressSpecValidationPatterns(t *testing.T) {
	t.Parallel()

	t.Run("fqdn accepts host and optional port", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, value := range []string{
			"konflux.example.com",
			"ui.example.com:8443",
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
