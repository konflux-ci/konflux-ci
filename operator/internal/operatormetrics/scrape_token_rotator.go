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

package operatormetrics

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

const (
	// OperatorNamespace is the deployment namespace for the Konflux operator manager.
	OperatorNamespace = "konflux-operator"
	// OperatorAppName is the app.kubernetes.io/name label for operator-owned resources.
	OperatorAppName = "konflux-operator"

	operatorMetricsReaderClusterRole = "konflux-operator-metrics-reader"
	operatorMetricsReaderBinding     = "konflux-operator-prometheus-konflux-operator-metrics-reader"
)

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=konflux-operator-prometheus-konflux-operator-metrics-reader,verbs=get;create;patch;bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=konflux-operator-metrics-reader,verbs=bind;escalate
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,resourceNames=controller-manager-metrics-monitor,verbs=get;create;patch

// ScrapeTokenRotator reconciles operator metrics scrape wiring: scraper CRB, scrape token, and ServiceMonitor.
type ScrapeTokenRotator struct {
	Client       client.Client
	Clock        clock.Clock
	TokenCreator kubernetes.TokenCreator
	Namespace    string
	Interval     time.Duration
}

// NeedLeaderElection ensures only the elected manager replica rotates the scrape token.
func (r *ScrapeTokenRotator) NeedLeaderElection() bool {
	return true
}

// Start runs until ctx is cancelled, reconciling on a fixed ticker interval.
func (r *ScrapeTokenRotator) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("operator-scrape-wiring")

	namespace := r.Namespace
	if namespace == "" {
		namespace = OperatorNamespace
	}

	interval := r.rotationInterval()

	if _, err := r.reconcile(ctx, namespace); err != nil {
		log.Error(err, "failed to reconcile operator metrics scrape wiring")
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := r.reconcile(ctx, namespace); err != nil {
				log.Error(err, "failed to reconcile operator metrics scrape wiring")
			}
		}
	}
}

func (r *ScrapeTokenRotator) reconcile(ctx context.Context, namespace string) (time.Duration, error) {
	clk := r.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}

	scraper := kubernetes.OperandMetricsScraperSA(namespace)
	if err := kubernetes.EnsureMetricsScraperServiceAccount(ctx, r.Client, namespace); err != nil {
		return 0, err
	}

	subjects := kubernetes.MetricsScraperBindingSubjects(namespace)
	if err := kubernetes.EnsureMetricsReaderBinding(
		ctx,
		r.Client,
		operatorMetricsReaderBinding,
		operatorMetricsReaderClusterRole,
		subjects,
	); err != nil {
		return 0, err
	}

	if err := EnsureOperatorServiceMonitor(ctx, r.Client, namespace); err != nil {
		return 0, err
	}

	requeue, err := kubernetes.EnsurePrometheusScrapeToken(ctx, kubernetes.EnsureScrapeTokenInput{
		Client:           r.Client,
		Clock:            clk,
		TokenCreator:     r.TokenCreator,
		Scraper:          scraper,
		OperandNamespace: namespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return kubernetes.ApplyScrapeTokenSecret(applyCtx, r.Client, secret)
		},
	})
	return requeue.RequeueAfter, err
}

func (r *ScrapeTokenRotator) rotationInterval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}
	return kubernetes.DefaultScrapeTokenRotationInterval
}
