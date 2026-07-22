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

// ValidateScrapeContract checks ServiceMonitor, scrape token, and metrics-reader CRB wiring.
// TEMP EXPERIMENT (experiment/uwm-no-sm-resync): does not require metrics-scrape-resync
// annotations; ValidateOperandScrapeResync asserts they are absent for component targets.
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

	skipVerify, caSecret, caKey, serverName, err := ServiceMonitorEndpointTLS(sm)
	if err != nil {
		return fmt.Errorf("servicemonitor tls: %w", err)
	}
	wantSkip := target.TLSInsecureSkipVerifyForScrape()
	if skipVerify != wantSkip {
		return fmt.Errorf("servicemonitor %s/%s insecureSkipVerify=%v, want %v",
			target.Namespace, sm.GetName(), skipVerify, wantSkip)
	}
	if !wantSkip {
		if caSecret != target.MetricsCASecret {
			return fmt.Errorf("servicemonitor %s/%s tls ca secret %q, want %q",
				target.Namespace, sm.GetName(), caSecret, target.MetricsCASecret)
		}
		if caKey != metricsauth.MetricsCACertKey {
			return fmt.Errorf("servicemonitor %s/%s tls ca key %q, want %q",
				target.Namespace, sm.GetName(), caKey, metricsauth.MetricsCACertKey)
		}
		wantServerName := target.TLSServerNameForScrape()
		if serverName != wantServerName {
			return fmt.Errorf("servicemonitor %s/%s serverName %q, want %q",
				target.Namespace, sm.GetName(), serverName, wantServerName)
		}
		if _, err := metricsauth.SecretBytes(ctx, c, target.Namespace, target.MetricsCASecret, metricsauth.MetricsCACertKey); err != nil {
			return fmt.Errorf("metrics CA secret: %w", err)
		}
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
