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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
// On OpenShift UWM, prometheus-operator can reject a ServiceMonitor when bearerTokenSecret is
// not visible at evaluation time and may not recover when the Secret appears later. A merge
// patch on resync annotations triggers re-processing without changing scrape spec.
//
// No-op when the ServiceMonitor CRD is absent or the object is not found. When Force is
// false and a resync annotation already exists, skips unless MarkSettlePending is set.
func ResyncOperandServiceMonitor(
	ctx context.Context,
	c client.Client,
	namespace, name string,
	opts ServiceMonitorResyncOptions,
) error {
	if namespace == "" || name == "" {
		return fmt.Errorf("serviceMonitor namespace and name are required")
	}
	clk := opts.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(serviceMonitorGVK)
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, existing)
	if meta.IsNoMatchError(err) {
		return nil
	}
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get ServiceMonitor %s/%s: %w", namespace, name, err)
	}

	if !opts.Force && !opts.MarkSettlePending && hasServiceMonitorResyncAnnotation(existing) {
		return nil
	}

	resyncAt := clk.Now().UTC().Format(time.RFC3339)
	patch := existing.DeepCopy()
	annotations := patch.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[ServiceMonitorResyncAnnotation] = resyncAt
	if opts.Reason != "" {
		annotations[ServiceMonitorResyncReasonAnnotation] = opts.Reason
	}
	if opts.SecretResourceVersion != "" {
		annotations[ServiceMonitorResyncSecretRVAnnotation] = opts.SecretResourceVersion
	}
	if opts.CAResourceVersion != "" {
		annotations[ServiceMonitorResyncCARVAnnotation] = opts.CAResourceVersion
	}
	if opts.MarkSettlePending {
		annotations[ServiceMonitorResyncSettleAnnotation] = serviceMonitorResyncSettlePending
	}
	if opts.ClearSettlePending {
		delete(annotations, ServiceMonitorResyncSettleAnnotation)
	}
	patch.SetAnnotations(annotations)

	if err := c.Patch(ctx, patch, client.MergeFrom(existing)); err != nil {
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("patch ServiceMonitor %s/%s: %w", namespace, name, err)
	}

	logf.FromContext(ctx).Info(
		"metrics scrape resync",
		"namespace", namespace,
		"servicemonitor", name,
		"reason", opts.Reason,
		"secretResourceVersion", opts.SecretResourceVersion,
		"caResourceVersion", opts.CAResourceVersion,
		"resyncAt", resyncAt,
	)
	return nil
}

func hasServiceMonitorResyncAnnotation(sm *unstructured.Unstructured) bool {
	if sm == nil {
		return false
	}
	annotations := sm.GetAnnotations()
	return annotations != nil && annotations[ServiceMonitorResyncAnnotation] != ""
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
