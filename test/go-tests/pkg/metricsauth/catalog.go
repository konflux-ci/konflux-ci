package metricsauth

import (
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

const (
	// TargetGroupOperator labels metrics for the Konflux operator controller.
	TargetGroupOperator = "operator"
	// TargetGroupComponent labels metrics for operand controllers (build-service, etc.).
	TargetGroupComponent = "component"
)

// Catalog holds scraper identity and scrape targets for cluster integration tests.
type Catalog struct {
	Scraper ScraperConfig `json:"scraper" yaml:"scraper"`
	Targets []Target      `json:"targets" yaml:"targets"`
}

// ScraperConfig identifies the service account that acts as Prometheus.
type ScraperConfig struct {
	Namespace      string `json:"namespace" yaml:"namespace"`
	ServiceAccount string `json:"serviceAccount" yaml:"serviceAccount"`
}

// Target describes one /metrics endpoint to scrape.
type Target struct {
	ID                       string   `json:"id" yaml:"id"`
	Group                    string   `json:"group,omitempty" yaml:"group,omitempty"`
	Namespace                string   `json:"namespace" yaml:"namespace"`
	Service                  string   `json:"service" yaml:"service"`
	Scheme                   string   `json:"scheme" yaml:"scheme"`
	PortName                 string   `json:"portName" yaml:"portName"`
	Port                     int32    `json:"port" yaml:"port"`
	Path                     string   `json:"path" yaml:"path"`
	TLSInsecureSkipVerify    *bool    `json:"tlsInsecureSkipVerify,omitempty" yaml:"tlsInsecureSkipVerify,omitempty"`
	MetricsReaderClusterRole string   `json:"metricsReaderClusterRole" yaml:"metricsReaderClusterRole"`
	ScrapeTokenSecret        string   `json:"scrapeTokenSecret,omitempty" yaml:"scrapeTokenSecret,omitempty"`
	BodyMustMatchAny         []string `json:"bodyMustMatchAny" yaml:"bodyMustMatchAny"`
}

// LoadCatalog reads metrics-targets.yaml from path.
func LoadCatalog(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog %s: %w", path, err)
	}
	var catalog Catalog
	if err := yaml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse catalog %s: %w", path, err)
	}
	if err := catalog.validate(); err != nil {
		return nil, err
	}
	return &catalog, nil
}

// DefaultCatalogPath returns test/fixtures/metrics-targets.yaml relative to repo root.
func DefaultCatalogPath(repoRoot string) string {
	return filepath.Join(repoRoot, "test", "fixtures", "metrics-targets.yaml")
}

func (c *Catalog) validate() error {
	if c.Scraper.Namespace == "" || c.Scraper.ServiceAccount == "" {
		return fmt.Errorf("catalog scraper namespace and serviceAccount are required")
	}
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
