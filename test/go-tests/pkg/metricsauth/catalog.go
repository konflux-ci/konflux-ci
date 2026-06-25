package metricsauth

import (
	"fmt"
)

const (
	// TargetGroupOperator labels metrics for the Konflux operator controller.
	TargetGroupOperator = "operator"
	// TargetGroupComponent labels metrics for operand controllers (build-service, etc.).
	TargetGroupComponent = "component"
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
	MetricsReaderClusterRole string
	ScrapeTokenSecret        string
	BodyMustMatchAny         []string
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
