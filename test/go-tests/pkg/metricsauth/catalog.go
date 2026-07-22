package metricsauth

import (
	"fmt"
)

const (
	// TargetGroupOperator labels metrics for the Konflux operator controller.
	TargetGroupOperator = "operator"
	// TargetGroupComponent labels metrics for operand controllers (build-service, etc.).
	TargetGroupComponent = "component"

	// MetricsServerCertSecretName is the TLS Secret with leaf (tls.crt/tls.key) and
	// scrape trust (ca.crt) for HTTPS metrics — single-Secret / konflux-issuer pattern.
	MetricsServerCertSecretName = "metrics-server-cert"
	// MetricsCASecretName is the Secret ServiceMonitors and integration tests trust via
	// tlsConfig.ca (key ca.crt). Same Secret as MetricsServerCertSecretName.
	MetricsCASecretName = MetricsServerCertSecretName
	// MetricsCACertKey is the CA certificate key in MetricsCASecretName.
	MetricsCACertKey = "ca.crt"
)

// Catalog holds scrape targets for cluster integration tests.
type Catalog struct {
	Targets []Target
}

// Target describes one /metrics endpoint to scrape.
type Target struct {
	ID                       string
	Group                    string
	Namespace                string
	Service                  string
	Scheme                   string
	PortName                 string
	Port                     int32
	Path                     string
	TLSInsecureSkipVerify    *bool
	// MetricsCASecret is the Secret name holding the CA used to verify HTTPS metrics
	// (typically MetricsCASecretName). Required when TLS verification is enabled.
	MetricsCASecret string
	// TLSServerName overrides the expected TLS server name. When empty and verifying,
	// defaults to <Service>.<Namespace>.svc.
	TLSServerName            string
	MetricsReaderClusterRole string
	ScrapeTokenSecret        string
	BodyMustMatchAny         []string
	// UWMUpCheck includes the target in OpenShift UWM up==1 specs outside the
	// scrape-token contract suite (legacy interim HTTP operands).
	UWMUpCheck bool
}

func (c *Catalog) validate() error {
	if len(c.Targets) == 0 {
		return fmt.Errorf("catalog must define at least one target")
	}
	for i := range c.Targets {
		if err := c.Targets[i].validate(); err != nil {
			return fmt.Errorf("target[%d] %q: %w", i, c.Targets[i].ID, err)
		}
	}
	return nil
}

func (t *Target) validate() error {
	switch {
	case t.ID == "":
		return fmt.Errorf("id is required")
	case t.Namespace == "":
		return fmt.Errorf("namespace is required")
	case t.Service == "":
		return fmt.Errorf("service is required")
	case t.PortName == "":
		return fmt.Errorf("portName is required")
	case t.Port == 0:
		return fmt.Errorf("port is required")
	case t.Path == "":
		return fmt.Errorf("path is required")
	case t.MetricsReaderClusterRole == "":
		return fmt.Errorf("metricsReaderClusterRole is required")
	case len(t.BodyMustMatchAny) == 0:
		return fmt.Errorf("bodyMustMatchAny is required")
	}
	if t.Scheme == "" {
		t.Scheme = "https"
	}
	if t.Scheme != "http" && t.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if t.Scheme == "https" && t.ScrapeTokenSecret == "" {
		return fmt.Errorf("scrapeTokenSecret is required for https targets")
	}
	if t.Scheme == "https" && !t.TLSInsecureSkipVerifyForScrape() && t.MetricsCASecret == "" {
		return fmt.Errorf("metricsCASecret is required when TLS verification is enabled")
	}
	if t.Group == "" {
		t.Group = TargetGroupComponent
	}
	switch t.Group {
	case TargetGroupOperator, TargetGroupComponent:
		return nil
	default:
		return fmt.Errorf("group must be %q or %q", TargetGroupOperator, TargetGroupComponent)
	}
}

// LabelGroup returns the Ginkgo label for this target's group (operator or component).
func (t Target) LabelGroup() string {
	if t.Group == "" {
		return TargetGroupComponent
	}
	return t.Group
}

// TLSInsecureSkipVerifyForScrape returns whether to skip TLS verification (HTTPS only).
func (t Target) TLSInsecureSkipVerifyForScrape() bool {
	if t.TLSInsecureSkipVerify != nil {
		return *t.TLSInsecureSkipVerify
	}
	return t.Scheme == "https"
}

// TLSServerNameForScrape returns the TLS server name used when verifying HTTPS scrapes.
func (t Target) TLSServerNameForScrape() string {
	if t.TLSServerName != "" {
		return t.TLSServerName
	}
	return fmt.Sprintf("%s.%s.svc", t.Service, t.Namespace)
}

// ScrapeTLSConfigFor returns TLS settings for a local scrape of this target.
// caCertPEM may be nil when InsecureSkipVerify is true.
func (t Target) ScrapeTLSConfigFor(caCertPEM []byte) ScrapeTLSConfig {
	return ScrapeTLSConfig{
		InsecureSkipVerify: t.TLSInsecureSkipVerifyForScrape(),
		ServerName:         t.TLSServerNameForScrape(),
		CACertPEM:          caCertPEM,
	}
}
