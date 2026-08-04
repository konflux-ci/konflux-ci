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

// Package ingress provides utilities for managing Ingress resources for KonfluxUI.
// It handles ingress creation, OpenShift domain detection, and hostname resolution.
package ingress

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

// ErrFQDNPortWithManagedIngress is returned when ingress.fqdn includes a :port
// suffix while ingress is effectively enabled. Managed Ingress rules omit ports,
// so auth redirects / ConsoleLink would disagree with the Ingress host.
var ErrFQDNPortWithManagedIngress = errors.New(
	"ingress.fqdn must not include a port when ingress is enabled; remove :port or set ingress.enabled=false",
)

const (
	// IngressName is the name of the Ingress resource created for KonfluxUI.
	IngressName = "konflux-ui"

	// ProxyServiceName is the name of the proxy service that the ingress routes to.
	ProxyServiceName = "proxy"

	// ProxyServicePort is the port name on the proxy service.
	ProxyServicePort = "web-tls"

	// DefaultIngressPath is the default path for the ingress rule.
	DefaultIngressPath = "/"

	// DefaultProxyHostname is the default hostname when ingress is not enabled.
	DefaultProxyHostname = "localhost"

	// DefaultProxyPort is the default port when ingress is not enabled.
	DefaultProxyPort = "9443"

	// OpenShift-specific annotations for TLS re-encryption
	annotationOpenShiftDestCACertSecret = "route.openshift.io/destination-ca-certificate-secret"
	annotationOpenShiftTermination      = "route.openshift.io/termination"
	terminationReencrypt                = "reencrypt"

	// destinationCASecretName is the name of the Secret containing the CA certificate
	// for TLS re-encryption on OpenShift. The Ingress-to-Route controller reads tls.crt
	// from this Secret to populate the Route's destinationCACertificate.
	// This Secret is created by cert-manager from the ui-ca Certificate resource
	// (see operator/upstream-kustomizations/ui/certmanager/certificate.yaml).
	destinationCASecretName = "ui-ca"
)

// Config holds the configuration for building an Ingress resource.
type Config struct {
	Name        string
	Namespace   string
	Hostname    string
	ServiceName string
	ServicePort string
	Path        string

	// IngressClassName specifies which IngressClass to use.
	IngressClassName *string

	// Annotations to add to the ingress resource.
	Annotations map[string]string

	// TLSSecretName is the name of the TLS secret to use.
	TLSSecretName string
}

// Build creates an Ingress resource from the given configuration.
// It always includes the OpenShift TLS re-encryption annotations.
func Build(cfg Config) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix

	// Start with the required OpenShift annotations for TLS re-encryption
	annotations := map[string]string{
		annotationOpenShiftDestCACertSecret: destinationCASecretName,
		annotationOpenShiftTermination:      terminationReencrypt,
	}

	// Merge user-provided annotations (user annotations take precedence)
	for k, v := range cfg.Annotations {
		annotations[k] = v
	}

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        cfg.Name,
			Namespace:   cfg.Namespace,
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: cfg.IngressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: cfg.Hostname,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     cfg.Path,
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: cfg.ServiceName,
											Port: networkingv1.ServiceBackendPort{
												Name: cfg.ServicePort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Add TLS configuration if a secret name is provided
	if cfg.TLSSecretName != "" {
		ingress.Spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{cfg.Hostname},
				SecretName: cfg.TLSSecretName,
			},
		}
	}

	return ingress
}

// BuildForUI creates an Ingress resource for KonfluxUI based on the spec, namespace, and hostname.
func BuildForUI(ui *konfluxv1alpha1.KonfluxUI, namespace, hostname string) *networkingv1.Ingress {
	ingressSpec := ui.Spec.GetIngress()

	return Build(Config{
		Name:             IngressName,
		Namespace:        namespace,
		Hostname:         hostname,
		ServiceName:      ProxyServiceName,
		ServicePort:      ProxyServicePort,
		Path:             DefaultIngressPath,
		IngressClassName: ingressSpec.IngressClassName,
		Annotations:      ingressSpec.Annotations,
		TLSSecretName:    ingressSpec.TLSSecretName,
	})
}

// GetOpenShiftIngressDomain retrieves the default ingress domain from OpenShift's ingresses.config resource.
func GetOpenShiftIngressDomain(ctx context.Context, c client.Client) (string, error) {
	// Use unstructured to get the OpenShift Ingress config without importing OpenShift types
	ingressConfig := &unstructured.Unstructured{}
	ingressConfig.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Ingress",
	})

	if err := c.Get(ctx, client.ObjectKey{Name: "cluster"}, ingressConfig); err != nil {
		return "", fmt.Errorf("failed to get OpenShift Ingress config: %w", err)
	}

	domain, found, err := unstructured.NestedString(ingressConfig.Object, "spec", "domain")
	if err != nil {
		return "", fmt.Errorf("failed to extract domain from Ingress config: %w", err)
	}
	if !found || domain == "" {
		return "", fmt.Errorf("domain not found in OpenShift Ingress config")
	}

	return domain, nil
}

// DetermineEndpointURL determines the endpoint URL for the UI based on ingress configuration.
//
// Resolution order:
//  1. FQDN set → use as the UI endpoint host. Optional :port is allowed only when ingress
//     is not effectively enabled; otherwise returns ErrFQDNPortWithManagedIngress. If
//     Hostname is also set, FQDN wins and a warning is logged.
//  2. Hostname set (FQDN empty) on OpenShift → compose <hostname>.<cluster-ingress-domain>
//     (no -<namespace> infix). Domain lookup failure fails reconcile.
//  3. Hostname set (FQDN empty) off OpenShift → ignore Hostname, log a warning, fall through.
//  4. Neither set (or Hostname ignored) → OpenShift + ingress effectively enabled uses
//     konflux-ui-<namespace>.<domain>; otherwise localhost:9443.
//
// FQDN and Hostname always define the UI endpoint URL independently of whether ingress is
// enabled, so users managing their own external routing can still configure auth redirects.
// Returns a *url.URL with Scheme set to "https" and Host set appropriately.
func DetermineEndpointURL(
	ctx context.Context,
	c client.Client,
	ui *konfluxv1alpha1.KonfluxUI,
	namespace string,
	clusterInfo *clusterinfo.Info,
) (*url.URL, error) {
	log := logf.FromContext(ctx)
	ingressSpec := ui.Spec.GetIngress()
	isOnOpenShift := clusterInfo != nil && clusterInfo.IsOpenShift()
	// Effectively enabled: explicit true, or unset (nil) on OpenShift.
	ingressEnabled := ptr.Deref(ingressSpec.Enabled, isOnOpenShift)

	// FQDN takes precedence over Hostname and over the ingress-enabled check.
	if ingressSpec.FQDN != "" {
		if ingressSpec.Hostname != "" {
			log.Info("Both ingress.fqdn and ingress.hostname are set; using fqdn and ignoring hostname",
				"fqdn", ingressSpec.FQDN, "hostname", ingressSpec.Hostname)
		}
		endpoint := &url.URL{
			Scheme: "https",
			Host:   ingressSpec.FQDN,
		}
		if endpoint.Port() != "" && ingressEnabled {
			return nil, ErrFQDNPortWithManagedIngress
		}
		return endpoint, nil
	}

	// Short hostname label: compose with cluster ingress domain on OpenShift only.
	if ingressSpec.Hostname != "" {
		if isOnOpenShift {
			domain, err := GetOpenShiftIngressDomain(ctx, c)
			if err != nil {
				return nil, fmt.Errorf("failed to get OpenShift ingress domain: %w", err)
			}
			return &url.URL{
				Scheme: "https",
				Host:   fmt.Sprintf("%s.%s", ingressSpec.Hostname, domain),
			}, nil
		}
		log.Info(
			"ingress.hostname ignored off OpenShift; using defaults (set ingress.fqdn instead)",
			"hostname", ingressSpec.Hostname,
		)
	}

	// If ingress is not enabled, use defaults
	if !ingressEnabled {
		return &url.URL{
			Scheme: "https",
			Host:   fmt.Sprintf("%s:%s", DefaultProxyHostname, DefaultProxyPort),
		}, nil
	}

	// If on OpenShift, try to get the default ingress domain
	if isOnOpenShift {
		domain, err := GetOpenShiftIngressDomain(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("failed to get OpenShift ingress domain: %w", err)
		}
		// Generate hostname using OpenShift naming convention: <name>-<namespace>.<domain>
		hostname := fmt.Sprintf("%s-%s.%s", IngressName, namespace, domain)
		return &url.URL{
			Scheme: "https",
			Host:   hostname,
		}, nil
	}

	// Fallback to defaults
	return &url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s:%s", DefaultProxyHostname, DefaultProxyPort),
	}, nil
}
