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
	"strconv"
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
	// Value is the RFC3339 UTC deadline after which settle-retry may run. Legacy
	// value "pending" (no deadline) is treated as already due.
	ServiceMonitorResyncSettleAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-settle"
	// ServiceMonitorResyncSettleRoundAnnotation tracks the current settle round
	// (0-indexed). Multi-round settle nudges prometheus-operator several times over
	// a window that covers observed UWM config-reload latency (~4-5 min).
	ServiceMonitorResyncSettleRoundAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-settle-round"

	// ServiceMonitor resync reason values (also logged and echoed in e2e artifacts).
	ServiceMonitorResyncReasonTokenMinted    = "token-minted"
	ServiceMonitorResyncReasonTokenRefreshed = "token-refreshed"
	ServiceMonitorResyncReasonSecretSync     = "secret-sync"
	ServiceMonitorResyncReasonCASync         = "ca-sync"
	ServiceMonitorResyncReasonSettleRetry    = "settle-retry"

	// DefaultServiceMonitorResyncSettleDelay is the first settle-retry delay (round 0).
	// Subsequent rounds use ServiceMonitorResyncSettleSchedule.
	DefaultServiceMonitorResyncSettleDelay = 15 * time.Second

	// serviceMonitorResyncSettlePendingLegacy is the pre-deadline settle marker.
	serviceMonitorResyncSettlePendingLegacy = "pending"
)

// ServiceMonitorResyncSettleSchedule defines the delays between successive
// settle-retry nudge rounds after a token mint/refresh. Each entry is the
// wait time before the next settle-retry fires. The cumulative schedule
// (15s, 45s, 105s, 225s, 345s from rotation) covers the observed 4-5 minute
// window during which UWM prometheus-operator may lag remounting
// bearerTokenSecret / reloading scrape config.
//
// Round 0 uses DefaultServiceMonitorResyncSettleDelay (15s).
var ServiceMonitorResyncSettleSchedule = []time.Duration{
	15 * time.Second,  // round 0: 15s after rotation
	30 * time.Second,  // round 1: 45s after rotation
	60 * time.Second,  // round 2: 105s after rotation
	120 * time.Second, // round 3: 225s after rotation
	120 * time.Second, // round 4: 345s after rotation
}

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
	// MarkSettlePending sets metrics-scrape-resync-settle to now+settle delay (RFC3339
	// deadline) until settle-retry clears it. When set, SettleRound determines
	// which schedule entry to use.
	MarkSettlePending bool
	// ClearSettlePending removes the settle-pending and settle-round annotations
	// (final settle-retry round).
	ClearSettlePending bool
	// SettleRound is the 0-indexed round number for multi-round settle. It
	// selects the delay from ServiceMonitorResyncSettleSchedule.
	SettleRound int
	Clock       clock.Clock
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
		delay := SettleDelayForRound(opts.SettleRound)
		deadline := clk.Now().UTC().Add(delay)
		annotations[ServiceMonitorResyncSettleAnnotation] = deadline.Format(time.RFC3339)
		annotations[ServiceMonitorResyncSettleRoundAnnotation] = strconv.Itoa(opts.SettleRound)
	}
	if opts.ClearSettlePending {
		delete(annotations, ServiceMonitorResyncSettleAnnotation)
		delete(annotations, ServiceMonitorResyncSettleRoundAnnotation)
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
// While pending, ReconcilePrometheusScrapeToken blocks secret-sync resyncs until the
// settle deadline elapses (see ServiceMonitorResyncSettleRemaining).
func ServiceMonitorResyncSettlePending(sm *unstructured.Unstructured) bool {
	if sm == nil {
		return false
	}
	annotations := sm.GetAnnotations()
	return annotations != nil && annotations[ServiceMonitorResyncSettleAnnotation] != ""
}

// ServiceMonitorResyncSettleRemaining returns how long until settle-retry may run.
// Zero means the settle nudge is due (or no settle is pending). Early Secret-watch
// reconciles must requeue this duration instead of clearing the settle marker.
func ServiceMonitorResyncSettleRemaining(sm *unstructured.Unstructured, now time.Time) time.Duration {
	if !ServiceMonitorResyncSettlePending(sm) {
		return 0
	}
	value := sm.GetAnnotations()[ServiceMonitorResyncSettleAnnotation]
	if value == "" || value == serviceMonitorResyncSettlePendingLegacy {
		// Legacy "pending" (or empty) has no deadline; treat as already due.
		return 0
	}
	deadline, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	if !now.Before(deadline) {
		return 0
	}
	return deadline.Sub(now)
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

// ServiceMonitorResyncSettleRound returns the current settle round (0-indexed)
// from the SM annotations. Returns 0 when the annotation is absent or invalid,
// which also covers the legacy single-round case.
func ServiceMonitorResyncSettleRound(sm *unstructured.Unstructured) int {
	if sm == nil {
		return 0
	}
	annotations := sm.GetAnnotations()
	if annotations == nil {
		return 0
	}
	raw := annotations[ServiceMonitorResyncSettleRoundAnnotation]
	if raw == "" {
		return 0
	}
	round, err := strconv.Atoi(raw)
	if err != nil || round < 0 {
		return 0
	}
	return round
}

// IsSettleFinalRound reports whether round is the last entry in the settle schedule.
func IsSettleFinalRound(round int) bool {
	return round >= len(ServiceMonitorResyncSettleSchedule)-1
}

// SettleDelayForRound returns the settle delay for the given round. Out-of-range
// rounds clamp to the last schedule entry.
func SettleDelayForRound(round int) time.Duration {
	if round < 0 {
		round = 0
	}
	if round >= len(ServiceMonitorResyncSettleSchedule) {
		round = len(ServiceMonitorResyncSettleSchedule) - 1
	}
	return ServiceMonitorResyncSettleSchedule[round]
}
