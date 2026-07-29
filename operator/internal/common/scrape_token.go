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
	"time"

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
	// is readable (deferred ServiceMonitor apply). Must be idempotent. Callers typically use
	// tc.ApplyOwned. Invoked when TLS is ready (create/update) and, while waiting for TLS, when
	// an SM already exists so tracking-client orphan cleanup retains it (retain vs create).
	ApplyServiceMonitor func(ctx context.Context) error
	// SecretReader loads the metrics TLS Secret for readiness checks and probes the operand
	// ServiceMonitor during TLS-wait retain. Prefer an uncached API reader so cert-manager
	// updates (and SM presence) are visible without waiting on the informer cache.
	// When nil, Client is used.
	SecretReader client.Reader
}

// DeferredSMApplyResult captures operand ServiceMonitor state from deferred apply without
// re-reading the informer cache immediately after the write.
type DeferredSMApplyResult struct {
	ExistedBeforeApply bool
	Prior              *unstructured.Unstructured
}

// ReconcilePrometheusScrapeToken ensures the operand scrape token Secret exists and is fresh.
// When ServiceMonitorName is set, it also applies the operand ServiceMonitor after the token
// is readable and metrics-server-cert is ready (deferred ServiceMonitor apply).
//
// Deferred ServiceMonitor apply: operand reconcilers skip the ServiceMonitor in applyManifests
// when componentMetrics is enabled. This function creates/updates the SM via ApplyServiceMonitor
// only after the scrape token Secret exists with non-empty token bytes and metrics-server-cert
// has verifying tls.crt + ca.crt (konflux-issuer single-Secret pattern). While waiting for TLS,
// if an SM already exists it is normally re-applied (retain vs create) so tracking-client orphan
// cleanup does not delete it during heal windows — except when metrics-server-cert is absent
// (MetricsTLSReasonCertMissing): retain is skipped so orphan cleanup removes the stale SM whose
// tlsConfig.ca references the missing Secret; deferred apply recreates it once the Secret
// verifies again. The SM is also re-applied on every successful scrape-token reconcile
// (idempotent SSA / orphan ownership).
//
// After the SM is applied, annotation-only "resync" patches nudge OpenShift UWM
// prometheus-operator to re-read bearerTokenSecret / CA material when the token is
// reminted or scrape-wiring Secret RVs change. Identical SM SSA alone does not enqueue UWM.
// EnsurePrometheusScrapeToken / EvaluateMetricsScrapeTLS results feed scrape readiness
// instead of re-reading Secrets from the informer cache immediately after writes.
// RequeueAfter follows the scrape-token TTL, or the remaining settle delay while a
// settle deadline is pending after TokenUpdated.
func ReconcilePrometheusScrapeToken(ctx context.Context, cfg ScrapeTokenReconcilerConfig) (reconcile.Result, error) {
	if err := validateScrapeTokenReconcilerConfig(cfg); err != nil {
		return reconcile.Result{}, err
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

	tlsResult, wait, err := ensureMetricsTLSReadyForServiceMonitor(ctx, cfg)
	if err != nil {
		return reconcile.Result{}, err
	}
	if wait != nil {
		// Retain an existing SM for orphan cleanup unless the metrics-server-cert Secret
		// is absent. When it is missing, skip retain so orphan cleanup drops the stale SM
		// (whose tlsConfig.ca references the absent Secret); deferred apply recreates the
		// SM once the Secret verifies again. For other not-ready reasons (empty, mismatch)
		// the Secret object still exists and CA refs resolve, so retain is safe.
		if tlsResult.Reason == kubernetes.MetricsTLSReasonCertMissing {
			logf.FromContext(ctx).Info(
				"skipping ServiceMonitor retain while metrics-server-cert is absent",
				"namespace", cfg.OperandNamespace,
				"servicemonitor", cfg.ServiceMonitorName,
			)
		} else {
			if retainErr := retainOperandServiceMonitorIfPresent(ctx, cfg); retainErr != nil {
				return reconcile.Result{}, retainErr
			}
		}
		return *wait, nil
	}

	smApply, err := applyOrProbeOperandServiceMonitor(ctx, cfg, tokenResult)
	if err != nil {
		return reconcile.Result{}, err
	}

	resyncOpts, settleWait := planServiceMonitorResync(cfg, tokenResult, tlsResult, smApply)
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
	// Secret watches can enqueue before RequeueAfter; keep the settle marker and
	// return the remaining delay from the same clock sample used for the due check.
	if settleWait > 0 {
		return reconcile.Result{RequeueAfter: settleWait}, nil
	}
	return reconcile.Result{RequeueAfter: tokenResult.RequeueAfter}, nil
}

func validateScrapeTokenReconcilerConfig(cfg ScrapeTokenReconcilerConfig) error {
	if cfg.TokenCreator == nil {
		return fmt.Errorf("token creator is required")
	}
	if cfg.OperandNamespace == "" {
		return fmt.Errorf("operand namespace is required")
	}
	if cfg.Scraper.Namespace == "" || cfg.Scraper.Name == "" {
		return fmt.Errorf("scraper service account is required")
	}
	return nil
}

func ensureMetricsTLSReadyForServiceMonitor(
	ctx context.Context,
	cfg ScrapeTokenReconcilerConfig,
) (kubernetes.MetricsScrapeTLSResult, *reconcile.Result, error) {
	tlsResult, err := kubernetes.ReconcileMetricsScrapeTLS(ctx, kubernetes.MetricsScrapeTLSInput{
		Client:    cfg.Client,
		Reader:    cfg.SecretReader,
		Namespace: cfg.OperandNamespace,
	})
	if err != nil {
		return kubernetes.MetricsScrapeTLSResult{}, nil, fmt.Errorf("reconcile metrics scrape TLS: %w", err)
	}
	if tlsResult.Ready {
		return tlsResult, nil, nil
	}
	logf.FromContext(ctx).V(1).Info(
		"metrics scrape deferred ServiceMonitor waiting for TLS chain",
		"namespace", cfg.OperandNamespace,
		"servicemonitor", cfg.ServiceMonitorName,
		"reason", tlsResult.Reason,
		"caResourceVersion", tlsResult.CAResourceVersion,
		"leafResourceVersion", tlsResult.LeafResourceVersion,
	)
	wait := reconcile.Result{RequeueAfter: kubernetes.DefaultMetricsTLSRequeue}
	return tlsResult, &wait, nil
}

// retainOperandServiceMonitorIfPresent re-applies an existing operand ServiceMonitor while
// TLS is not ready so tracking-client CleanupOrphans does not treat it as an orphan.
// If the SM is absent, it does nothing (first-boot create stays gated on TLS readiness).
func retainOperandServiceMonitorIfPresent(ctx context.Context, cfg ScrapeTokenReconcilerConfig) error {
	if cfg.ApplyServiceMonitor == nil || cfg.ServiceMonitorName == "" {
		return nil
	}

	reader := client.Reader(cfg.Client)
	if cfg.SecretReader != nil {
		reader = cfg.SecretReader
	}
	if reader == nil {
		return fmt.Errorf("client is required to retain ServiceMonitor during TLS wait")
	}

	smKey := client.ObjectKey{Namespace: cfg.OperandNamespace, Name: cfg.ServiceMonitorName}
	probe := &unstructured.Unstructured{}
	probe.SetGroupVersionKind(operandServiceMonitorGVK)
	switch err := reader.Get(ctx, smKey, probe); {
	case err == nil:
		// Continue to ApplyServiceMonitor below.
	case apierrors.IsNotFound(err), meta.IsNoMatchError(err):
		return nil
	default:
		return fmt.Errorf("probe ServiceMonitor %q for TLS-wait retain: %w", cfg.ServiceMonitorName, err)
	}

	logf.FromContext(ctx).V(2).Info(
		"retaining existing ServiceMonitor while waiting for metrics TLS chain",
		"namespace", cfg.OperandNamespace,
		"servicemonitor", cfg.ServiceMonitorName,
	)
	if err := cfg.ApplyServiceMonitor(ctx); err != nil {
		return fmt.Errorf("retain ServiceMonitor %q during TLS wait: %w", cfg.ServiceMonitorName, err)
	}
	return nil
}

func applyOrProbeOperandServiceMonitor(
	ctx context.Context,
	cfg ScrapeTokenReconcilerConfig,
	tokenResult kubernetes.EnsureScrapeTokenResult,
) (DeferredSMApplyResult, error) {
	smKey := client.ObjectKey{Namespace: cfg.OperandNamespace, Name: cfg.ServiceMonitorName}
	if cfg.ApplyServiceMonitor != nil {
		return applyDeferredOperandServiceMonitor(ctx, cfg, tokenResult, smKey)
	}
	return probeOperandServiceMonitor(ctx, cfg.Client, smKey), nil
}

// planServiceMonitorResync decides annotation nudges and any settle wait from a
// single clock sample. Sampling twice (due-check then requeue) can miss settle-retry
// when the deadline crosses between calls.
func planServiceMonitorResync(
	cfg ScrapeTokenReconcilerConfig,
	tokenResult kubernetes.EnsureScrapeTokenResult,
	tlsResult kubernetes.MetricsScrapeTLSResult,
	smApply DeferredSMApplyResult,
) (kubernetes.ServiceMonitorResyncOptions, time.Duration) {
	secretRV := tokenResult.ResourceVersion
	caRV := tlsResult.CAResourceVersion
	var storedSecretRV string
	var storedCARV string
	var settlePending bool
	if smApply.ExistedBeforeApply && smApply.Prior != nil {
		storedSecretRV = kubernetes.ServiceMonitorResyncSecretRV(smApply.Prior)
		storedCARV = kubernetes.ServiceMonitorResyncCARV(smApply.Prior)
		settlePending = kubernetes.ServiceMonitorResyncSettlePending(smApply.Prior)
	}

	clk := cfg.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	now := clk.Now()

	resyncOpts := kubernetes.ServiceMonitorResyncOptions{Clock: cfg.Clock}
	var settleWait time.Duration
	switch {
	case tokenResult.TokenUpdated:
		resyncOpts.Force = true
		if tokenResult.SecretExisted {
			resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonTokenRefreshed
		} else {
			resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonTokenMinted
		}
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.CAResourceVersion = caRV
		resyncOpts.MarkSettlePending = true
	case settlePending:
		// Wait for the settle deadline; early Secret-watch reconciles must not
		// clear the marker or issue settle-retry before DefaultServiceMonitorResyncSettleDelay.
		settleWait = kubernetes.ServiceMonitorResyncSettleRemaining(smApply.Prior, now)
		if settleWait > 0 {
			break
		}
		resyncOpts.Force = true
		resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonSettleRetry
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.CAResourceVersion = caRV
		resyncOpts.ClearSettlePending = true
	case secretRV != "" && secretRV != storedSecretRV && !settlePending:
		resyncOpts.Force = true
		resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonSecretSync
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.CAResourceVersion = caRV
	case caRV != "" && caRV != storedCARV && !settlePending:
		resyncOpts.Force = true
		resyncOpts.Reason = kubernetes.ServiceMonitorResyncReasonCASync
		resyncOpts.SecretResourceVersion = secretRV
		resyncOpts.CAResourceVersion = caRV
	}
	return resyncOpts, settleWait
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
		logApply = log.V(2).Info
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
