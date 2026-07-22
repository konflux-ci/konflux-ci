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
)

const (
	// ServiceMonitorResyncAnnotation is a historical annotation key. Operand reconcilers
	// do not write it; OpenShift contract tests assert it is absent.
	ServiceMonitorResyncAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync"
	// ServiceMonitorResyncReasonAnnotation is unused (see ServiceMonitorResyncAnnotation).
	ServiceMonitorResyncReasonAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-reason"
	// ServiceMonitorResyncSecretRVAnnotation is unused (see ServiceMonitorResyncAnnotation).
	//nolint:gosec // G101: annotation key, not a credential
	ServiceMonitorResyncSecretRVAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-secret-rv"
	// ServiceMonitorResyncCARVAnnotation is unused (see ServiceMonitorResyncAnnotation).
	//nolint:gosec // G101: annotation key, not a credential
	ServiceMonitorResyncCARVAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-ca-rv"
	// ServiceMonitorResyncSettleAnnotation is unused (see ServiceMonitorResyncAnnotation).
	ServiceMonitorResyncSettleAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-settle"

	// Historical reason string constants retained for callers/tests.
	ServiceMonitorResyncReasonTokenMinted    = "token-minted"
	ServiceMonitorResyncReasonTokenRefreshed = "token-refreshed"
	ServiceMonitorResyncReasonSecretSync     = "secret-sync"
	ServiceMonitorResyncReasonCASync         = "ca-sync"
	ServiceMonitorResyncReasonSettleRetry    = "settle-retry"

	// DefaultServiceMonitorResyncSettleDelay is unused; settle-retry requeues are not used.
	DefaultServiceMonitorResyncSettleDelay = 15 * time.Second

	serviceMonitorResyncSettlePending = "pending"
)

var serviceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

// ServiceMonitorResyncOptions configures a call to ResyncOperandServiceMonitor.
//
// Annotation nudges are not applied; the options type and reason constants remain for
// call-site compatibility and for e2e evidence helpers that assert annotations are absent.
type ServiceMonitorResyncOptions struct {
	// Force is retained for call-site compatibility; ResyncOperandServiceMonitor ignores it.
	Force bool
	// Reason is retained for call-site compatibility; no annotation is written.
	Reason string
	// SecretResourceVersion is retained for call-site compatibility.
	SecretResourceVersion string
	// CAResourceVersion is retained for call-site compatibility.
	CAResourceVersion string
	// MarkSettlePending is retained for call-site compatibility.
	MarkSettlePending bool
	// ClearSettlePending is retained for call-site compatibility.
	ClearSettlePending bool
	Clock              clock.Clock
}

// ResyncOperandServiceMonitor previously patched ServiceMonitor annotations to nudge
// OpenShift UWM prometheus-operator. It is intentionally a no-op: deferred ServiceMonitor
// apply prevents SM-before-Secret rejection, and idempotent SM re-apply on reconcile covers
// scrape continuity when the token or metrics TLS Secret changes. Callers may still invoke
// it; no annotations are written.
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
	_ = ctx
	_ = opts
	return nil
}

// ServiceMonitorResyncSettlePending reports whether a historical settle-pending annotation
// is present. Operand reconcilers do not set it.
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
