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

package kubernetes

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ServiceMonitorResyncAnnotation records the last time the operand reconciler
	// nudged user-workload prometheus-operator to re-process a ServiceMonitor.
	// See operator/docs/component-monitoring.md (resync nudges).
	ServiceMonitorResyncAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync"
	// ServiceMonitorResyncReasonAnnotation records why the last resync nudge ran.
	ServiceMonitorResyncReasonAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-reason"
	// ServiceMonitorResyncSecretRVAnnotation records the scrape-token Secret
	// resourceVersion last seen when the SM was nudged.
	//nolint:gosec // G101: annotation key, not a credential
	ServiceMonitorResyncSecretRVAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-secret-rv"
	// ServiceMonitorResyncCARVAnnotation records the metrics-ca Secret resourceVersion
	// last seen when the SM was nudged (so UWM refreshes tls-assets on CA change).
	//nolint:gosec // G101: annotation key, not a credential
	ServiceMonitorResyncCARVAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-ca-rv"
	// ServiceMonitorResyncSettleAnnotation marks a pending delayed settle nudge.
	ServiceMonitorResyncSettleAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-settle"

	// ServiceMonitor resync reason values (also logged and echoed in e2e artifacts).
	ServiceMonitorResyncReasonTokenMinted    = "token-minted"
	ServiceMonitorResyncReasonTokenRefreshed = "token-refreshed"
	ServiceMonitorResyncReasonSecretSync     = "secret-sync"
	ServiceMonitorResyncReasonCASync         = "ca-sync"
	ServiceMonitorResyncReasonSettleRetry    = "settle-retry"

	// DefaultServiceMonitorResyncSettleDelay waits before a settle-retry SM patch.
	DefaultServiceMonitorResyncSettleDelay = 15 * time.Second

	serviceMonitorResyncSettlePending = "pending"
)

var serviceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

// ServiceMonitorResyncOptions configures an operand ServiceMonitor resync patch.
//
// Resync patches are annotation-only nudges so prometheus-operator re-evaluates the SM
// after the scrape token is readable. Callers in ReconcilePrometheusScrapeToken set
// Reason and SecretResourceVersion; MarkSettlePending/ClearSettlePending coordinate the
// settle-retry requeue so secret-sync does not race ahead of settle-retry.
type ServiceMonitorResyncOptions struct {
	// Force patches even when a prior resync annotation exists.
	Force bool
	// Reason is stored in ServiceMonitorResyncReasonAnnotation (token-minted, token-refreshed,
	// settle-retry, secret-sync, ca-sync).
	Reason string
	// SecretResourceVersion is stored in ServiceMonitorResyncSecretRVAnnotation.
	SecretResourceVersion string
	// CAResourceVersion is stored in ServiceMonitorResyncCARVAnnotation.
	CAResourceVersion string
	// MarkSettlePending sets metrics-scrape-resync-settle=pending until settle-retry clears it.
	MarkSettlePending bool
	// ClearSettlePending removes the settle-pending annotation (settle-retry path).
	ClearSettlePending bool
	Clock              clock.Clock
}

// ResyncOperandServiceMonitor patches operand ServiceMonitor annotations so prometheus-operator
// re-evaluates scrape configuration.
//
// TEMP EXPERIMENT (branch experiment/uwm-no-sm-resync): intentionally a no-op. Deferred SM
// apply is unchanged; this arm measures whether annotation resync / settle-retry are needed
// for UWM boot and token/CA rotation. See .local/uwm-resync-shrink-test-plan.md and PR that
// targets this experiment. Do not merge as a permanent product change without that A/B.
func ResyncOperandServiceMonitor(
	ctx context.Context,
	c client.Client,
	namespace, name string,
	opts ServiceMonitorResyncOptions,
) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("serviceMonitor namespace and name are required")
	}
	_ = c
	logf.FromContext(ctx).Info(
		"metrics scrape resync skipped (experiment/uwm-no-sm-resync)",
		"namespace", namespace,
		"servicemonitor", name,
		"reason", opts.Reason,
	)
	return nil
}

// ServiceMonitorResyncSettlePending reports whether a delayed settle-retry nudge is pending.
// While pending, ReconcilePrometheusScrapeToken blocks secret-sync resyncs.
func ServiceMonitorResyncSettlePending(sm *unstructured.Unstructured) bool {
	if sm == nil {
		return false
	}
	annotations := sm.GetAnnotations()
	return annotations != nil &&
		annotations[ServiceMonitorResyncSettleAnnotation] == serviceMonitorResyncSettlePending
}

// ServiceMonitorResyncSecretRV returns the secret resourceVersion recorded on the SM.
func ServiceMonitorResyncSecretRV(sm *unstructured.Unstructured) string {
	if sm == nil {
		return ""
	}
	annotations := sm.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[ServiceMonitorResyncSecretRVAnnotation]
}

// ServiceMonitorResyncCARV returns the metrics-ca resourceVersion recorded on the SM.
func ServiceMonitorResyncCARV(sm *unstructured.Unstructured) string {
	if sm == nil {
		return ""
	}
	annotations := sm.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[ServiceMonitorResyncCARVAnnotation]
}
