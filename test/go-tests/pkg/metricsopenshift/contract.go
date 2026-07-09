package metricsopenshift

import (
	"context"
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

// ValidateScrapeContract checks ServiceMonitor, scrape token, metrics-reader CRB wiring, and
// that operand reconcilers set the metrics-scrape-resync annotation.
func ValidateScrapeContract(ctx context.Context, c client.Reader, target metricsauth.Target) error {
	sm, err := GetServiceMonitor(ctx, c, target.Namespace, ServiceMonitorName(target))
	if err != nil {
		return fmt.Errorf("serviceMonitor: %w", err)
	}
	if err := ValidateOperandScrapeResync(sm, target); err != nil {
		return err
	}

	scheme, bearerSecret, err := ServiceMonitorEndpointScheme(sm)
	if err != nil {
		return err
	}
	if scheme != "https" {
		return fmt.Errorf("servicemonitor %s/%s endpoint scheme %q, want https", target.Namespace, sm.GetName(), scheme)
	}
	if bearerSecret != kubernetes.ScrapeTokenSecretName {
		return fmt.Errorf("servicemonitor %s/%s bearerTokenSecret %q, want %q",
			target.Namespace, sm.GetName(), bearerSecret, kubernetes.ScrapeTokenSecretName)
	}

	if _, err := metricsauth.SecretToken(ctx, c, target.Namespace, target.ScrapeTokenSecret, kubernetes.ScrapeTokenSecretKey); err != nil {
		return fmt.Errorf("scrape token secret: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if err := c.Get(ctx, types.NamespacedName{Name: PrometheusBindingName(target)}, crb); err != nil {
		return fmt.Errorf("clusterRoleBinding: %w", err)
	}
	if crb.Annotations[kubernetes.MetricsScraperBindingAnnotation] != "true" {
		return fmt.Errorf("clusterRoleBinding %q missing %s annotation", crb.Name, kubernetes.MetricsScraperBindingAnnotation)
	}
	if crb.RoleRef.Name != target.MetricsReaderClusterRole {
		return fmt.Errorf("clusterRoleBinding %q roleRef %q, want %q", crb.Name, crb.RoleRef.Name, target.MetricsReaderClusterRole)
	}

	wantSA := kubernetes.OperandMetricsScraperSA(target.Namespace)
	for _, subject := range crb.Subjects {
		if subject.Kind == rbacv1.ServiceAccountKind &&
			subject.Name == wantSA.Name &&
			subject.Namespace == wantSA.Namespace {
			return nil
		}
	}
	return fmt.Errorf("clusterRoleBinding %q subjects missing metrics-scraper SA in %s", crb.Name, target.Namespace)
}
