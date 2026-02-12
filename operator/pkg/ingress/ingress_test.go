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

package ingress

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

func TestBuild(t *testing.T) {
	t.Run("creates ingress with basic config", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
		}

		ing := Build(cfg)

		g.Expect(ing.Name).To(gomega.Equal("test-ingress"))
		g.Expect(ing.Namespace).To(gomega.Equal("test-ns"))
		g.Expect(ing.Kind).To(gomega.Equal("Ingress"))
		g.Expect(ing.APIVersion).To(gomega.Equal("networking.k8s.io/v1"))
		g.Expect(ing.Spec.Rules).To(gomega.HaveLen(1))
		g.Expect(ing.Spec.Rules[0].Host).To(gomega.Equal("example.com"))
	})

	t.Run("includes OpenShift TLS re-encryption annotations by default", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
		}

		ing := Build(cfg)

		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"route.openshift.io/destination-ca-certificate-secret", "ui-ca"))
		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"route.openshift.io/termination", "reencrypt"))
	})

	t.Run("merges user annotations with default annotations", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
			Annotations: map[string]string{
				"custom-annotation": "custom-value",
			},
		}

		ing := Build(cfg)

		// Should have default OpenShift annotations
		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"route.openshift.io/destination-ca-certificate-secret", "ui-ca"))
		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"route.openshift.io/termination", "reencrypt"))
		// Should have custom annotation
		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"custom-annotation", "custom-value"))
	})

	t.Run("user annotations can override default annotations", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
			Annotations: map[string]string{
				"route.openshift.io/termination": "edge",
			},
		}

		ing := Build(cfg)

		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"route.openshift.io/termination", "edge"))
	})

	t.Run("sets ingress class name when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ingressClassName := "nginx"
		cfg := Config{
			Name:             "test-ingress",
			Namespace:        "test-ns",
			Hostname:         "example.com",
			ServiceName:      "test-svc",
			ServicePort:      "http",
			Path:             "/",
			IngressClassName: &ingressClassName,
		}

		ing := Build(cfg)

		g.Expect(ing.Spec.IngressClassName).NotTo(gomega.BeNil())
		g.Expect(*ing.Spec.IngressClassName).To(gomega.Equal("nginx"))
	})

	t.Run("does not set ingress class name when nil", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
		}

		ing := Build(cfg)

		g.Expect(ing.Spec.IngressClassName).To(gomega.BeNil())
	})

	t.Run("configures TLS when secret name is provided", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:          "test-ingress",
			Namespace:     "test-ns",
			Hostname:      "example.com",
			ServiceName:   "test-svc",
			ServicePort:   "http",
			Path:          "/",
			TLSSecretName: "my-tls-secret",
		}

		ing := Build(cfg)

		g.Expect(ing.Spec.TLS).To(gomega.HaveLen(1))
		g.Expect(ing.Spec.TLS[0].SecretName).To(gomega.Equal("my-tls-secret"))
		g.Expect(ing.Spec.TLS[0].Hosts).To(gomega.ContainElement("example.com"))
	})

	t.Run("does not configure TLS when secret name is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http",
			Path:        "/",
		}

		ing := Build(cfg)

		g.Expect(ing.Spec.TLS).To(gomega.BeNil())
	})

	t.Run("configures HTTP rule path correctly", func(t *testing.T) {
		g := gomega.NewWithT(t)

		cfg := Config{
			Name:        "test-ingress",
			Namespace:   "test-ns",
			Hostname:    "example.com",
			ServiceName: "test-svc",
			ServicePort: "http-port",
			Path:        "/api",
		}

		ing := Build(cfg)

		g.Expect(ing.Spec.Rules[0].HTTP.Paths).To(gomega.HaveLen(1))
		path := ing.Spec.Rules[0].HTTP.Paths[0]
		g.Expect(path.Path).To(gomega.Equal("/api"))
		g.Expect(*path.PathType).To(gomega.Equal(networkingv1.PathTypePrefix))
		g.Expect(path.Backend.Service.Name).To(gomega.Equal("test-svc"))
		g.Expect(path.Backend.Service.Port.Name).To(gomega.Equal("http-port"))
	})
}

func TestBuildForUI(t *testing.T) {
	t.Run("creates ingress with default settings", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "konflux-ui",
			},
			Spec: konfluxv1alpha1.KonfluxUISpec{},
		}

		ing := BuildForUI(ui, "konflux-ui", "ui.example.com")

		g.Expect(ing.Name).To(gomega.Equal(IngressName))
		g.Expect(ing.Namespace).To(gomega.Equal("konflux-ui"))
		g.Expect(ing.Spec.Rules[0].Host).To(gomega.Equal("ui.example.com"))
		g.Expect(ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name).To(gomega.Equal(ProxyServiceName))
		g.Expect(ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Port.Name).To(gomega.Equal(ProxyServicePort))
		g.Expect(ing.Spec.Rules[0].HTTP.Paths[0].Path).To(gomega.Equal(DefaultIngressPath))
	})

	t.Run("uses ingress spec values when provided", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ingressClassName := "openshift-default"
		ui := &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "konflux-ui",
			},
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled:          ptr.To(true),
					IngressClassName: &ingressClassName,
					Annotations: map[string]string{
						"nginx.ingress.kubernetes.io/proxy-body-size": "100m",
					},
					TLSSecretName: "ui-tls-cert",
				},
			},
		}

		ing := BuildForUI(ui, "my-namespace", "ui.example.com")

		g.Expect(ing.Namespace).To(gomega.Equal("my-namespace"))
		g.Expect(*ing.Spec.IngressClassName).To(gomega.Equal("openshift-default"))
		g.Expect(ing.Annotations).To(gomega.HaveKeyWithValue(
			"nginx.ingress.kubernetes.io/proxy-body-size", "100m"))
		g.Expect(ing.Spec.TLS).To(gomega.HaveLen(1))
		g.Expect(ing.Spec.TLS[0].SecretName).To(gomega.Equal("ui-tls-cert"))
	})

	t.Run("uses provided hostname", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "konflux-ui",
			},
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
					Host:    "explicit.host.com",
				},
			},
		}

		ing := BuildForUI(ui, "konflux-ui", "determined.host.com")

		// Should use the hostname passed in (the already-determined hostname)
		g.Expect(ing.Spec.Rules[0].Host).To(gomega.Equal("determined.host.com"))
	})
}

func TestGetOpenShiftIngressDomain(t *testing.T) {
	t.Run("returns domain from OpenShift Ingress config", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ingressConfig := &unstructured.Unstructured{}
		ingressConfig.SetGroupVersionKind(networkingv1.SchemeGroupVersion.WithKind("Ingress"))
		ingressConfig.SetGroupVersionKind(openshiftIngressGVK())
		ingressConfig.SetName("cluster")
		_ = unstructured.SetNestedField(ingressConfig.Object, "apps.openshift.example.com", "spec", "domain")

		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingressConfig).
			Build()

		domain, err := GetOpenShiftIngressDomain(context.Background(), fakeClient)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(domain).To(gomega.Equal("apps.openshift.example.com"))
	})

	t.Run("returns error when Ingress config not found", func(t *testing.T) {
		g := gomega.NewWithT(t)

		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		_, err := GetOpenShiftIngressDomain(context.Background(), fakeClient)

		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get OpenShift Ingress config"))
	})

	t.Run("returns error when domain field is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ingressConfig := &unstructured.Unstructured{}
		ingressConfig.SetGroupVersionKind(openshiftIngressGVK())
		ingressConfig.SetName("cluster")
		// domain field is not set

		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingressConfig).
			Build()

		_, err := GetOpenShiftIngressDomain(context.Background(), fakeClient)

		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("domain not found"))
	})
}

func TestDetermineEndpointURL(t *testing.T) {
	t.Run("returns defaults when ingress is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: nil,
			},
		}

		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nil)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal(DefaultProxyHostname))
		g.Expect(endpoint.Port()).To(gomega.Equal(DefaultProxyPort))
		g.Expect(endpoint.String()).To(gomega.Equal("https://localhost:9443"))
	})

	t.Run("returns defaults when ingress is not enabled", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(false),
				},
			},
		}

		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nil)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal(DefaultProxyHostname))
		g.Expect(endpoint.Port()).To(gomega.Equal(DefaultProxyPort))
	})

	t.Run("returns explicit host when ingress is disabled but host is specified", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(false),
					Host:    "my-custom-host.example.com",
				},
			},
		}

		// Even with ingress disabled, explicit host should be honored
		// (e.g., user managing their own Gateway API or external routing)
		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nil)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal("my-custom-host.example.com"))
		g.Expect(endpoint.Port()).To(gomega.Equal("")) // No port for standard TLS
		g.Expect(endpoint.String()).To(gomega.Equal("https://my-custom-host.example.com"))
	})

	t.Run("returns explicit host when ingress is enabled with host specified", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
					Host:    "my-custom-host.example.com",
				},
			},
		}

		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nil)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal("my-custom-host.example.com"))
		g.Expect(endpoint.Port()).To(gomega.Equal("")) // No port for standard ingress TLS
		g.Expect(endpoint.String()).To(gomega.Equal("https://my-custom-host.example.com"))
	})

	t.Run("returns explicit host on OpenShift instead of generating hostname", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
					Host:    "my-custom-host.example.com",
				},
			},
		}

		openShiftClusterInfo := createOpenShiftClusterInfo()

		// Even on OpenShift, explicit host should take priority over auto-generated hostname
		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", openShiftClusterInfo)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal("my-custom-host.example.com"))
		g.Expect(endpoint.Port()).To(gomega.Equal(""))
		g.Expect(endpoint.String()).To(gomega.Equal("https://my-custom-host.example.com"))
	})

	t.Run("generates OpenShift hostname when ingress enabled without host on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
				},
			},
		}

		ingressConfig := &unstructured.Unstructured{}
		ingressConfig.SetGroupVersionKind(openshiftIngressGVK())
		ingressConfig.SetName("cluster")
		_ = unstructured.SetNestedField(ingressConfig.Object, "apps.openshift.example.com", "spec", "domain")

		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingressConfig).
			Build()

		openShiftClusterInfo := createOpenShiftClusterInfo()

		endpoint, err := DetermineEndpointURL(
			context.Background(), fakeClient, ui, "konflux-ui", openShiftClusterInfo)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal("konflux-ui-konflux-ui.apps.openshift.example.com"))
		g.Expect(endpoint.Port()).To(gomega.Equal(""))
	})

	t.Run("returns defaults when not on OpenShift and no host specified", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
				},
			},
		}

		// clusterInfo is nil - not on OpenShift
		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nil)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal(DefaultProxyHostname))
		g.Expect(endpoint.Port()).To(gomega.Equal(DefaultProxyPort))
	})

	t.Run("returns defaults when clusterInfo indicates not OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
				},
			},
		}

		nonOpenShiftClusterInfo := createNonOpenShiftClusterInfo()

		endpoint, err := DetermineEndpointURL(
			context.Background(), nil, ui, "konflux-ui", nonOpenShiftClusterInfo)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal(DefaultProxyHostname))
		g.Expect(endpoint.Port()).To(gomega.Equal(DefaultProxyPort))
	})

	t.Run("returns error when OpenShift ingress domain fetch fails", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
				},
			},
		}

		// Empty client with no OpenShift Ingress config
		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			Build()

		openShiftClusterInfo := createOpenShiftClusterInfo()

		_, err := DetermineEndpointURL(
			context.Background(), fakeClient, ui, "konflux-ui", openShiftClusterInfo)

		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get OpenShift ingress domain"))
	})

	t.Run("uses different namespace in generated OpenShift hostname", func(t *testing.T) {
		g := gomega.NewWithT(t)

		ui := &konfluxv1alpha1.KonfluxUI{
			Spec: konfluxv1alpha1.KonfluxUISpec{
				Ingress: &konfluxv1alpha1.IngressSpec{
					Enabled: ptr.To(true),
				},
			},
		}

		ingressConfig := &unstructured.Unstructured{}
		ingressConfig.SetGroupVersionKind(openshiftIngressGVK())
		ingressConfig.SetName("cluster")
		_ = unstructured.SetNestedField(ingressConfig.Object, "apps.cluster.example.com", "spec", "domain")

		scheme := runtime.NewScheme()
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(ingressConfig).
			Build()

		openShiftClusterInfo := createOpenShiftClusterInfo()

		endpoint, err := DetermineEndpointURL(
			context.Background(), fakeClient, ui, "custom-namespace", openShiftClusterInfo)

		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(endpoint.Hostname()).To(gomega.Equal("konflux-ui-custom-namespace.apps.cluster.example.com"))
	})
}

func TestConstants(t *testing.T) {
	t.Run("verifies constant values", func(t *testing.T) {
		g := gomega.NewWithT(t)

		g.Expect(IngressName).To(gomega.Equal("konflux-ui"))
		g.Expect(ProxyServiceName).To(gomega.Equal("proxy"))
		g.Expect(ProxyServicePort).To(gomega.Equal("web-tls"))
		g.Expect(DefaultIngressPath).To(gomega.Equal("/"))
		g.Expect(DefaultProxyHostname).To(gomega.Equal("localhost"))
		g.Expect(DefaultProxyPort).To(gomega.Equal("9443"))
	})
}

// Helper functions for tests

// openshiftIngressGVK returns the GroupVersionKind for OpenShift's Ingress config.
func openshiftIngressGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
	}
}

// mockDiscoveryClient implements clusterinfo.DiscoveryClient for testing.
type mockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(gv string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[gv]; ok {
		return r, nil
	}
	return &metav1.APIResourceList{}, nil
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}

// createOpenShiftClusterInfo creates a cluster info that indicates OpenShift.
func createOpenShiftClusterInfo() *clusterinfo.Info {
	mock := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "ClusterVersion"},
				},
			},
		},
		serverVersion: &version.Info{GitVersion: "v1.29.0"},
	}

	info, _ := clusterinfo.DetectWithClient(mock)
	return info
}

// createNonOpenShiftClusterInfo creates a cluster info that indicates non-OpenShift.
func createNonOpenShiftClusterInfo() *clusterinfo.Info {
	mock := &mockDiscoveryClient{
		resources:     map[string]*metav1.APIResourceList{},
		serverVersion: &version.Info{GitVersion: "v1.29.0"},
	}

	info, _ := clusterinfo.DetectWithClient(mock)
	return info
}

// Verify that our code doesn't require specific runtime.Object interface
var _ client.Object = (*unstructured.Unstructured)(nil)
