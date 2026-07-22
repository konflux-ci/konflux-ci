package metricsopenshift

import (
	"context"
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxkubernetes "github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

// OperandScrapeResyncExpected reports whether the operand reconciler should set
// konflux.konflux-ci.dev/metrics-scrape-resync on the ServiceMonitor.
func OperandScrapeResyncExpected(target metricsauth.Target) bool {
	return target.ScrapeTokenSecret != "" && target.LabelGroup() == metricsauth.TargetGroupComponent
}

// ServiceMonitorResyncAt returns the operand scrape-resync annotation timestamp, if set.
func ServiceMonitorResyncAt(sm *unstructured.Unstructured) string {
	if sm == nil {
		return ""
	}
	annotations := sm.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[konfluxkubernetes.ServiceMonitorResyncAnnotation]
}

// ServiceMonitorResyncReason returns the operand scrape-resync reason annotation, if set.
func ServiceMonitorResyncReason(sm *unstructured.Unstructured) string {
	if sm == nil {
		return ""
	}
	annotations := sm.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[konfluxkubernetes.ServiceMonitorResyncReasonAnnotation]
}

// ValidateOperandScrapeResync checks scrape-resync annotation expectations for the
// ServiceMonitor.
//
// TEMP EXPERIMENT (experiment/uwm-no-sm-resync): annotation resync is disabled, so
// component targets must NOT have metrics-scrape-resync set. Evidence logging still
// records MISSING resync_at while UWM up/sm_after_secret remain the health signals.
func ValidateOperandScrapeResync(sm *unstructured.Unstructured, target metricsauth.Target) error {
	if !OperandScrapeResyncExpected(target) {
		return nil
	}
	if at := ServiceMonitorResyncAt(sm); at != "" {
		return fmt.Errorf("servicemonitor %s/%s unexpectedly has %q=%q (experiment arm disables annotation resync)",
			target.Namespace, sm.GetName(), konfluxkubernetes.ServiceMonitorResyncAnnotation, at)
	}
	return nil
}

// LogScrapeResyncEvidence logs scrape-resync state on every run (pass or fail) for CI artifacts.
// Uses stdout like wait.go so Prow build logs capture output even when specs pass.
//
// Each line includes resync_at, resync_reason, secret/SM resource versions, uwm_active_targets,
// and sm_after_secret (true when the ServiceMonitor was created after prometheus-scrape-token).
// sm_after_secret=true is the deferred SM apply ordering fingerprint; resync_reason alone is not a
// reliable flake indicator.
func LogScrapeResyncEvidence(
	ctx context.Context,
	cfg *rest.Config,
	kube client.Reader,
	targets []metricsauth.Target,
) {
	fmt.Println("[UWM resync] operand ServiceMonitor scrape-resync evidence (before metrics specs):")

	var promTargets *TargetsResult
	if cfg != nil {
		if result, err := FetchPrometheusTargets(ctx, cfg); err == nil {
			promTargets = result
		}
	}

	for _, target := range targets {
		if !OperandScrapeResyncExpected(target) {
			continue
		}
		fmt.Printf("[UWM resync]   %s\n", formatScrapeResyncEvidenceLine(ctx, cfg, kube, promTargets, target))
	}
}

func formatScrapeResyncEvidenceLine(
	ctx context.Context,
	cfg *rest.Config,
	kube client.Reader,
	promTargets *TargetsResult,
	target metricsauth.Target,
) string {
	smName := ServiceMonitorName(target)
	resyncAt := "MISSING"
	resyncReason := "MISSING"
	smRV := "unknown"
	smCreated := "unknown"
	var sm *unstructured.Unstructured
	if got, err := GetServiceMonitor(ctx, kube, target.Namespace, smName); err != nil {
		resyncAt = fmt.Sprintf("error:%v", err)
		resyncReason = resyncAt
		smRV = resyncAt
		smCreated = resyncAt
	} else {
		sm = got
		smRV = sm.GetResourceVersion()
		smCreated = formatTimestamp(sm.GetCreationTimestamp())
		if at := ServiceMonitorResyncAt(sm); at != "" {
			resyncAt = at
		}
		if reason := ServiceMonitorResyncReason(sm); reason != "" {
			resyncReason = reason
		}
	}

	tokenState := "absent"
	secretRV := "unknown"
	secretCreated := "unknown"
	smAfterSecret := "unknown"
	secret := &corev1.Secret{}
	if err := kube.Get(ctx, types.NamespacedName{Namespace: target.Namespace, Name: target.ScrapeTokenSecret}, secret); err != nil {
		tokenState = fmt.Sprintf("error:%v", err)
		secretRV = tokenState
		secretCreated = tokenState
	} else if _, err := metricsauth.SecretToken(ctx, kube, target.Namespace, target.ScrapeTokenSecret, konfluxkubernetes.ScrapeTokenSecretKey); err == nil {
		tokenState = "present"
		secretRV = secret.ResourceVersion
		secretCreated = formatTimestamp(secret.CreationTimestamp)
		if sm != nil {
			smCreatedAt := sm.GetCreationTimestamp().Time
			secretCreatedAt := secret.CreationTimestamp.Time
			if !smCreatedAt.IsZero() && !secretCreatedAt.IsZero() {
				smAfterSecret = strconv.FormatBool(!smCreatedAt.Before(secretCreatedAt))
			}
		}
	} else {
		tokenState = fmt.Sprintf("error:%v", err)
		secretRV = secret.ResourceVersion
		secretCreated = formatTimestamp(secret.CreationTimestamp)
	}

	activeTargets := "unknown"
	if promTargets != nil {
		activeTargets = strconv.Itoa(CountTargetsForNamespace(promTargets, target.Namespace))
	}

	upState := "unknown"
	if cfg != nil {
		if result, err := QueryPrometheus(ctx, cfg, UpPromQL(target)); err != nil {
			upState = fmt.Sprintf("error:%v", err)
		} else {
			upState = FormatUpResult(result)
		}
	}

	return fmt.Sprintf(
		"id=%s sm=%s sm_rv=%s sm_created=%s secret_created=%s sm_after_secret=%s resync_at=%s resync_reason=%s secret_rv=%s scrape_token=%s uwm_active_targets=%s uwm_up=%s",
		target.ID, smName, smRV, smCreated, secretCreated, smAfterSecret,
		resyncAt, resyncReason, secretRV, tokenState, activeTargets, upState,
	)
}
