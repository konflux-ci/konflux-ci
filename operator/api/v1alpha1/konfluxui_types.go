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
	"fmt"
	"net/url"
	"strconv"

	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodePortServiceSpec defines the NodePort service configuration for the proxy.
type NodePortServiceSpec struct {
	// HTTPSPort is the NodePort to use for the HTTPS port.
	// If not specified, Kubernetes will allocate a port automatically.
	// This is useful for exposing Konflux UI to the outside world without an Ingress controller.
	// +optional
	// +kubebuilder:validation:Minimum=30000
	// +kubebuilder:validation:Maximum=32767
	HTTPSPort *int32 `json:"httpsPort,omitempty"`
}

// IngressSpec defines the ingress configuration for KonfluxUI.
type IngressSpec struct {
	// Enabled controls whether an Ingress resource should be created.
	// When nil (unset), defaults to true on OpenShift, false otherwise.
	// +optional
	// +nullable
	Enabled *bool `json:"enabled,omitempty"`
	// IngressClassName specifies which IngressClass to use for the ingress.
	// +optional
	IngressClassName *string `json:"ingressClassName,omitempty"`
	// Host is the hostname used as the endpoint for configuring oauth2-proxy, dex, and related components.
	// When set, this hostname is always used regardless of whether ingress is enabled,
	// allowing users who manage their own external routing (e.g., Gateway API, hardware LB)
	// to configure the endpoint without the operator managing an Ingress resource.
	// On OpenShift, if empty, the default ingress domain and naming convention will be used.
	// +optional
	Host string `json:"host,omitempty"`
	// Annotations to add to the ingress resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// TLSSecretName is the name of the Kubernetes TLS secret to use for the ingress.
	// If not specified, TLS will not be configured on the ingress.
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`
	// NodePortService configures the proxy Service as a NodePort type.
	// When set, the proxy Service will be exposed via NodePort instead of ClusterIP.
	// This is useful for accessing Konflux UI from outside the cluster without an Ingress controller.
	// +optional
	NodePortService *NodePortServiceSpec `json:"nodePortService,omitempty"`
}

// ProxyDeploymentSpec defines customizations for the proxy deployment.
type ProxyDeploymentSpec struct {
	// Replicas is the number of replicas for the proxy deployment.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas,omitempty"`
	// ReverseProxy defines customizations for the reverse proxy container.
	// +optional
	ReverseProxy *ContainerSpec `json:"reverseProxy,omitempty"`
	// OAuth2Proxy defines customizations for the oauth2-proxy container.
	// +optional
	OAuth2Proxy *ContainerSpec `json:"oauth2Proxy,omitempty"`
	// Endpoints configures optional backend services that the proxy routes to.
	// Each endpoint can be independently enabled and customized.
	// +optional
	Endpoints *ProxyEndpointsSpec `json:"endpoints,omitempty"`
}

// ProxyEndpointsSpec configures optional backend endpoints proxied by the UI reverse proxy.
type ProxyEndpointsSpec struct {
	// Kite enables the Kite plugin endpoint.
	// When enabled, requests to /api/k8s/plugins/kite/ are proxied to the Kite backend.
	// +optional
	Kite *EndpointSpec `json:"kite,omitempty"`
	// KubeArchive enables the KubeArchive plugin endpoint.
	// When enabled, requests to /api/k8s/plugins/kubearchive/ are proxied to the KubeArchive backend.
	// +optional
	KubeArchive *EndpointSpec `json:"kubearchive,omitempty"`
	// Watson enables the Watson chatbot endpoint.
	// When enabled, requests to /api/chatbot/ are proxied to the IBM Watson Assistant API.
	// +optional
	Watson *WatsonEndpointSpec `json:"watson,omitempty"`
}

// EndpointSpec configures an optional in-cluster backend endpoint.
type EndpointSpec struct {
	// Enabled controls whether this endpoint is active.
	Enabled bool `json:"enabled"`
	// Hostname overrides the default backend service address.
	// +optional
	Hostname string `json:"hostname,omitempty"`
}

// WatsonEndpointSpec configures the Watson chatbot endpoint.
type WatsonEndpointSpec struct {
	// Enabled controls whether the Watson chatbot endpoint is active.
	Enabled bool `json:"enabled"`
	// Hostname overrides the Watson API host.
	// Defaults to api.us-east.assistant.watson.cloud.ibm.com.
	// +optional
	Hostname string `json:"hostname,omitempty"`
	// SecretName is the name of the Secret containing the Watson API key.
	// The Secret must have a key named API_KEY with the pre-encoded Basic auth
	// value (e.g. base64("apikey:<your-api-key>")).
	// +kubebuilder:default="watson-api-key"
	// +kubebuilder:validation:MinLength=1
	SecretName string `json:"secretName,omitempty"`
}

// DexDeploymentSpec defines customizations for the dex deployment.
type DexDeploymentSpec struct {
	// Replicas is the number of replicas for the dex deployment.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	Replicas int32 `json:"replicas,omitempty"`
	// Dex defines customizations for the dex container.
	// +optional
	Dex *ContainerSpec `json:"dex,omitempty"`
	// Config defines the Dex IdP configuration parameters.
	// +optional
	Config *dex.DexParams `json:"config,omitempty"`
}

// RuntimeConfigSpec defines frontend runtime configuration for the Konflux UI.
// Each field maps to a window.KONFLUX_RUNTIME property injected into the SPA
// via runtime-config.js. The All() iterator provides the canonical mapping
// from typed fields to environment variable names.
type RuntimeConfigSpec struct {
	// ChatBot configures the AI chatbot feature in the UI.
	// +optional
	ChatBot *ChatBotConfig `json:"chatBot,omitempty"`
	// Monitoring configures error monitoring (e.g. Sentry) for the UI frontend.
	// +optional
	Monitoring *MonitoringConfig `json:"monitoring,omitempty"`
}

// ChatBotConfig configures the AI chatbot feature in the UI.
type ChatBotConfig struct {
	// Enabled controls whether the chatbot UI is visible to users.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// MonitoringConfig configures error monitoring (e.g. Sentry) for the UI frontend.
type MonitoringConfig struct {
	// Enabled controls whether error monitoring is active.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
	// DSN is the data source name (e.g. Sentry DSN) for error reporting.
	// +optional
	DSN string `json:"dsn,omitempty"`
	// Environment identifies the deployment environment (e.g. "staging", "production").
	// +optional
	Environment string `json:"environment,omitempty"`
	// Cluster identifies the cluster name for error reports.
	// +optional
	Cluster string `json:"cluster,omitempty"`
	// SampleRateErrors controls the error event sample rate (0.0 to 1.0).
	// +optional
	// +kubebuilder:validation:Pattern=`^(0(\.\d+)?|1(\.0+)?)$`
	SampleRateErrors string `json:"sampleRateErrors,omitempty"`
}

// All iterates over all set runtime config entries, yielding the environment
// variable name and its string value. Only fields with non-nil/non-empty values
// are yielded. This is the single source of truth for the field-to-env-var mapping.
func (r *RuntimeConfigSpec) All(yield func(key, value string) bool) {
	if r.ChatBot != nil && r.ChatBot.Enabled != nil {
		if !yield("RUNTIME_CHAT_BOT_ENABLED", strconv.FormatBool(*r.ChatBot.Enabled)) {
			return
		}
	}
	if r.Monitoring != nil {
		m := r.Monitoring
		if m.Enabled != nil {
			if !yield("RUNTIME_MONITORING_ENABLED", strconv.FormatBool(*m.Enabled)) {
				return
			}
		}
		if m.DSN != "" {
			if !yield("RUNTIME_MONITORING_DSN", m.DSN) {
				return
			}
		}
		if m.Environment != "" {
			if !yield("RUNTIME_MONITORING_ENVIRONMENT", m.Environment) {
				return
			}
		}
		if m.Cluster != "" {
			if !yield("RUNTIME_MONITORING_CLUSTER", m.Cluster) {
				return
			}
		}
		if m.SampleRateErrors != "" {
			if !yield("RUNTIME_MONITORING_SAMPLE_RATE_ERRORS", m.SampleRateErrors) {
				return
			}
		}
	}
}

// KonfluxUISpec defines the desired state of KonfluxUI
type KonfluxUISpec struct {
	// Ingress defines the ingress configuration for KonfluxUI.
	// This affects the proxy, oauth2-proxy, and dex components.
	// +optional
	// +nullable
	Ingress *IngressSpec `json:"ingress,omitempty"`
	// Proxy defines customizations for the proxy deployment.
	// +optional
	Proxy *ProxyDeploymentSpec `json:"proxy,omitempty"`
	// Dex defines customizations for the dex deployment.
	// +optional
	Dex *DexDeploymentSpec `json:"dex,omitempty"`
	// RuntimeConfig defines frontend runtime configuration for the Konflux UI.
	// These settings are injected as window.KONFLUX_RUNTIME properties in the SPA.
	// +optional
	RuntimeConfig *RuntimeConfigSpec `json:"runtimeConfig,omitempty"`

	// ComponentMetrics controls Prometheus scrape resources for this component.
	// Set by the Konflux reconciler from spec.componentMetrics on the Konflux CR.
	// +optional
	ComponentMetrics *ComponentMetricsConfig `json:"componentMetrics,omitempty"`
}

// IngressStatus defines the observed state of the Ingress configuration.
type IngressStatus struct {
	// Enabled indicates whether the Ingress resource is enabled.
	Enabled bool `json:"enabled"`
	// Hostname is the hostname configured for the ingress.
	// This is the actual hostname being used, whether explicitly configured or auto-generated.
	// +optional
	Hostname string `json:"hostname,omitempty"`
	// URL is the full URL to access the KonfluxUI.
	// +optional
	URL string `json:"url,omitempty"`
}

// KonfluxUIStatus defines the observed state of KonfluxUI
type KonfluxUIStatus struct {
	// Conditions represent the latest available observations of the KonfluxUI state
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Ingress contains the observed state of the Ingress configuration.
	// +optional
	Ingress *IngressStatus `json:"ingress,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'konflux-ui'",message="KonfluxUI CR must be named 'konflux-ui'. Only one instance is allowed per cluster."

// KonfluxUI is the Schema for the konfluxuis API
type KonfluxUI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:default:={}
	Spec   KonfluxUISpec   `json:"spec"`
	Status KonfluxUIStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KonfluxUIList contains a list of KonfluxUI
type KonfluxUIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KonfluxUI `json:"items"`
}

// GetConditions returns the conditions from the KonfluxUI status.
func (k *KonfluxUI) GetConditions() []metav1.Condition {
	return k.Status.Conditions
}

// SetConditions sets the conditions on the KonfluxUI status.
func (k *KonfluxUI) SetConditions(conditions []metav1.Condition) {
	k.Status.Conditions = conditions
}

// -----------------------------------------------------------------------------
// Spec Accessor Methods
// These methods provide safe access to optional fields with sensible defaults,
// reducing nil checks throughout the codebase.
// -----------------------------------------------------------------------------

// GetIngress returns the IngressSpec with safe defaults if nil.
func (s *KonfluxUISpec) GetIngress() IngressSpec {
	if s.Ingress == nil {
		return IngressSpec{}
	}
	return *s.Ingress
}

// GetNodePortService returns the NodePortServiceSpec if configured, nil otherwise.
func (s *KonfluxUISpec) GetNodePortService() *NodePortServiceSpec {
	if s.Ingress == nil {
		return nil
	}
	return s.Ingress.NodePortService
}

// GetProxy returns the ProxyDeploymentSpec with safe defaults if nil.
func (s *KonfluxUISpec) GetProxy() ProxyDeploymentSpec {
	if s.Proxy == nil {
		return ProxyDeploymentSpec{Replicas: 1}
	}
	return *s.Proxy
}

// GetDex returns the DexDeploymentSpec with safe defaults if nil.
func (s *KonfluxUISpec) GetDex() DexDeploymentSpec {
	if s.Dex == nil {
		return DexDeploymentSpec{Replicas: 1}
	}
	return *s.Dex
}

// -----------------------------------------------------------------------------
// High-level Convenience Methods on KonfluxUI
// These methods encapsulate common conditional checks used throughout the controller.
// -----------------------------------------------------------------------------

// GetIngressEnabledPreference returns the user's preference for ingress.
// Returns nil if not explicitly configured, allowing callers to apply defaults.
func (k *KonfluxUI) GetIngressEnabledPreference() *bool {
	return k.Spec.GetIngress().Enabled
}

// HasDexConfig returns true if custom Dex configuration is provided.
func (k *KonfluxUI) HasDexConfig() bool {
	return k.Spec.GetDex().Config != nil
}

// GetOpenShiftLoginPreference returns the user's preference for OpenShift login.
// Returns nil if not explicitly configured, allowing callers to apply defaults.
func (k *KonfluxUI) GetOpenShiftLoginPreference() *bool {
	config := k.Spec.GetDex().Config
	if config == nil {
		return nil
	}
	return config.ConfigureLoginWithOpenShift
}

// ResolveDexEndpoint returns the effective Dex endpoint URL.
// If the CR specifies a hostname override, it uses that; otherwise, it returns the default endpoint.
func (k *KonfluxUI) ResolveDexEndpoint(defaultEndpoint *url.URL) *url.URL {
	config := k.Spec.GetDex().Config
	if config == nil || config.Hostname == "" {
		return defaultEndpoint
	}

	host := config.Hostname
	if config.Port != "" {
		host = fmt.Sprintf("%s:%s", host, config.Port)
	}

	return &url.URL{
		Scheme: "https",
		Host:   host,
	}
}
