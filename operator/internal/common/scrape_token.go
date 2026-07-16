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

package common

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

var operandServiceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

// ScrapeTokenReconcilerConfig configures operand-namespace Prometheus scrape token reconciliation.
type ScrapeTokenReconcilerConfig struct {
	Client             client.Client
	Clock              clock.Clock
	TokenCreator       kubernetes.TokenCreator
	Scraper            types.NamespacedName
	OperandNamespace   string
	ServiceMonitorName string
	Apply              kubernetes.ScrapeTokenApplyFunc
	// ApplyServiceMonitor applies the operand ServiceMonitor after prometheus-scrape-token
	// is readable (deferred ServiceMonitor apply). Must be idempotent and invoked on every
	// reconcile so the tracking client retains the SM during orphan cleanup; callers
	// typically use tc.ApplyOwned.
	ApplyServiceMonitor func(ctx context.Context) error
}

// DeferredSMApplyResult captures operand ServiceMonitor state from deferred apply without
// re-reading the informer cache immediately after the write.
type DeferredSMApplyResult struct {
	ExistedBeforeApply bool
	Prior              *unstructured.Unstructured
}

// ReconcilePrometheusScrapeToken ensures the operand scrape token Secret exists and is fresh.
// When ServiceMonitorName is set, it also applies the operand ServiceMonitor after the token
// is readable (deferred ServiceMonitor apply) and nudges prometheus-operator to re-evaluate
// the SM (resync nudges).
//
// Deferred ServiceMonitor apply: operand reconcilers skip the ServiceMonitor in applyManifests
// when componentMetrics is enabled. This function applies it here via ApplyServiceMonitor
// only after the scrape token Secret exists with non-empty token bytes. ApplyServiceMonitor
// must run every reconcile (not only on create) so tracking-client orphan cleanup does not
// delete the SM.
//
// Resync nudges: patches SM annotations via ResyncOperandServiceMonitor when the token is
// minted (token-minted) or refreshed (token-refreshed), on settle requeue after
// DefaultServiceMonitorResyncSettleDelay (settle-retry), or when secret resourceVersion
// drifts (secret-sync). secret-sync is suppressed while metrics-scrape-resync-settle=pending.
//
// Returns RequeueAfter on mint/refresh so settle-retry runs once more before steady state.
func ReconcilePrometheusScrapeToken(ctx context.Context, cfg ScrapeTokenReconcilerConfig) (reconcile.Result, error) {
	if cfg.TokenCreator == nil {
		return reconcile.Result{}, fmt.Errorf("token creator is required")
	}
	if cfg.OperandNamespace == "" {
		return reconcile.Result{}, fmt.Errorf("operand namespace is required")
	}
	if cfg.Scraper.Namespace == "" || cfg.Scraper.Name == "" {
		return reconcile.Result{}, fmt.Errorf("scraper service account is required")
	}

	tokenResult, err := kubernetes.EnsurePrometheusScrapeToken(ctx, kubernetes.EnsureScrapeTokenInput{
		Client:           cfg.Client,
		Clock:            cfg.Clock,
		TokenCreator:     cfg.TokenCreator,
		Scraper:          cfg.Scraper,
		OperandNamespace: cfg.OperandNamespace,
		Apply:            cfg.Apply,
	})
	if err != nil {
		return reconcile.Result{}, err
	}
	if cfg.ServiceMonitorName == "" {
		return reconcile.Result{RequeueAfter: tokenResult.RequeueAfter}, nil
	}

	smKey := client.ObjectKey{Namespace: cfg.OperandNamespace, Name: cfg.ServiceMonitorName}
	var smApply DeferredSMApplyResult
	if cfg.ApplyServiceMonitor != nil {
		var applyErr error
		smApply, applyErr = applyDeferredOperandServiceMonitor(ctx, cfg, tokenResult, smKey)
		if applyErr != nil {
			return reconcile.Result{}, applyErr
		}
	} else {
		smApply = probeOperandServiceMonitor(ctx, cfg.Client, smKey)
		if !smApply.ExistedBeforeApply && tokenResult.TokenUpdated {
			return reconcile.Result{RequeueAfter: kubernetes.DefaultServiceMonitorResyncSettleDelay}, nil
		}
	}

	secretRV := tokenResult.ResourceVersion
	var storedSecretRV string
	var settlePending bool
	if smApply.ExistedBeforeApply && smApply.Prior != nil {
		storedSecretRV = kubernetes.ServiceMonitorResyncSecretRV(smApply.Prior)
		settlePending = kubernetes.ServiceMonitorResyncSettlePending(smApply.Prior)
	}
	if secretRV != "" && secretRV != storedSecretRV && settlePending {
		logf.FromContext(ctx).Info(
			"metrics scrape resync secret-sync deferred",
			"namespace", cfg.OperandNamespace,
			"servicemonitor", cfg.ServiceMonitorName,
			"secretResourceVersion", secretRV,
			"storedSecretResourceVersion", storedSecretRV,
			"settlePending", true,
		)
	}

	resyncOpts := kubernetes.ServiceMonitorResyncOptions{Clock: cfg.Clock}
	switch {
	case tokenResult.TokenUpdated:
		resyncOpts.Force = true
		if tokenResult.SecretExisted {
			resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonTokenRefreshed
		} else {
			resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonTokenMinted
		}
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.MarkSettlePending = true
	case settlePending:
		resyncOpts.Force = true
		resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonSettleRetry
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.ClearSettlePending = true
	case secretRV != "" && secretRV != storedSecretRV && !settlePending:
		resyncOpts.Force = true
		resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonSecretSync
		resyncOpts.SecretResourceVersion = secretRV
	}

	if resyncOpts.Force || resyncOpts.MarkSettlePending {
		if resyncErr := kubernetes.ResyncOperandServiceMonitor(
			ctx,
			cfg.Client,
			cfg.OperandNamespace,
			cfg.ServiceMonitorName,
			resyncOpts,
		); resyncErr != nil {
			return reconcile.Result{}, fmt.Errorf("resync servicemonitor %q: %w", cfg.ServiceMonitorName, resyncErr)
		}
	}

	if tokenResult.TokenUpdated {
		return reconcile.Result{RequeueAfter: kubernetes.DefaultServiceMonitorResyncSettleDelay}, nil
	}
	return reconcile.Result{RequeueAfter: tokenResult.RequeueAfter}, nil
}

// applyDeferredOperandServiceMonitor invokes ApplyServiceMonitor after the scrape token is
// readable. Deferred ServiceMonitor apply ensures prometheus-operator first sees the SM when
// bearerTokenSecret exists. Re-applies on every reconcile for tracking-client orphan retention.
func applyDeferredOperandServiceMonitor(
	ctx context.Context,
	cfg ScrapeTokenReconcilerConfig,
	tokenResult kubernetes.EnsureScrapeTokenResult,
	smKey client.ObjectKey,
) (DeferredSMApplyResult, error) {
	if cfg.ApplyServiceMonitor == nil {
		return DeferredSMApplyResult{}, nil
	}

	log := logf.FromContext(ctx)
	result := DeferredSMApplyResult{}
	existedKnown := true
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(operandServiceMonitorGVK)
	switch probeErr := cfg.Client.Get(ctx, smKey, probe); {
	case probeErr == nil:
		result.ExistedBeforeApply = true
		result.Prior = probe.DeepCopy()
	case apierrors.IsNotFound(probeErr), meta.IsNoMatchError(probeErr):
	default:
		existedKnown = false
		log.V(1).Info(
			"metrics scrape ServiceMonitor probe failed; continuing apply",
			"namespace", cfg.OperandNamespace,
			"servicemonitor", cfg.ServiceMonitorName,
			"error", probeErr,
		)
	}

	logApply := log.Info
	if existedKnown && result.ExistedBeforeApply {
		logApply = log.V(1).Info
	}
	logApply(
		"metrics scrape deferred ServiceMonitor apply",
		"namespace", cfg.OperandNamespace,
		"servicemonitor", cfg.ServiceMonitorName,
		"existedBeforeApply", result.ExistedBeforeApply,
		"secretResourceVersion", tokenResult.ResourceVersion,
	)
	if applyErr := cfg.ApplyServiceMonitor(ctx); applyErr != nil {
		return DeferredSMApplyResult{}, fmt.Errorf("apply ServiceMonitor %q: %w", cfg.ServiceMonitorName, applyErr)
	}
	return result, nil
}

func probeOperandServiceMonitor(ctx context.Context, c client.Client, smKey client.ObjectKey) DeferredSMApplyResult {
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(operandServiceMonitorGVK)
	switch err := c.Get(ctx, smKey, probe); {
	case err == nil:
		return DeferredSMApplyResult{
			ExistedBeforeApply: true,
			Prior:              probe.DeepCopy(),
		}
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		return DeferredSMApplyResult{}
	default:
		return DeferredSMApplyResult{}
	}
}
