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
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/internal/common"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

const (
	// OperatorNamespace is the deployment namespace for the Konflux operator manager.
	OperatorNamespace = "konflux-operator"
	// OperatorAppName is the app.kubernetes.io/name label for operator-owned resources.
	OperatorAppName = "konflux-operator"
	// OperatorMetricsServiceName is the metrics Service name after kustomize namePrefix.
	OperatorMetricsServiceName = "konflux-operator-controller-manager-metrics-service"
	// MetricsServerCertSecretName is the leaf TLS Secret mounted by the metrics server.
	MetricsServerCertSecretName = kubernetes.MetricsServerCertSecretName
	// MetricsCASecretName is the Secret ServiceMonitors trust (ca.crt). Same Secret as
	// MetricsServerCertSecretName; pods mount tls.crt/tls.key only.
	MetricsCASecretName = kubernetes.MetricsCASecretName
	// MetricsCACertKey is the CA certificate key in MetricsCASecretName.
	MetricsCACertKey = kubernetes.MetricsCACertKey

	operatorMetricsReaderClusterRole = "konflux-operator-metrics-reader"
	operatorMetricsReaderBinding     = "konflux-operator-prometheus-konflux-operator-metrics-reader"
)

// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,resourceNames=konflux-operator-prometheus-konflux-operator-metrics-reader,verbs=get;create;patch;bind
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=konflux-operator-metrics-reader,verbs=bind;escalate
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,resourceNames=controller-manager-metrics-monitor,verbs=get;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,resourceNames=metrics-server-cert,verbs=get;list;watch

// ScrapeTokenRotator reconciles operator metrics scrape wiring: scraper CRB, scrape token, and ServiceMonitor.
type ScrapeTokenRotator struct {
	Client client.Client
	// SecretReader loads metrics TLS Secrets without relying on a possibly stale informer
	// cache. When nil, Client is used.
	SecretReader client.Reader
	// Cache optionally wakes reconcile early when scrape-wiring Secrets change
	// (metrics-server-cert, prometheus-scrape-token) so TLS/token updates do not
	// wait for the default rotation interval.
	Cache        ctrlcache.Cache
	Clock        clock.Clock
	TokenCreator kubernetes.TokenCreator
	Namespace    string
	Interval     time.Duration
}

// NeedLeaderElection ensures only the elected manager replica rotates the scrape token.
func (r *ScrapeTokenRotator) NeedLeaderElection() bool {
	return true
}

// Start runs until ctx is cancelled, reconciling on an adaptive interval (TLS/token requeue
// short-circuits the default rotation period) and on scrape-wiring Secret events when Cache
// is set.
func (r *ScrapeTokenRotator) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("operator-scrape-wiring")

	namespace := r.Namespace
	if namespace == "" {
		namespace = OperatorNamespace
	}

	wake := make(chan struct{}, 1)
	r.watchScrapeWiringSecrets(ctx, namespace, wake)

	for {
		requeue, err := r.reconcile(ctx, namespace)
		if err != nil {
			log.Error(err, "failed to reconcile operator metrics scrape wiring")
		}

		wait := r.rotationInterval()
		if requeue > 0 && requeue < wait {
			wait = requeue
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		case <-wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (r *ScrapeTokenRotator) watchScrapeWiringSecrets(ctx context.Context, namespace string, wake chan struct{}) {
	if r.Cache == nil {
		return
	}
	log := logf.FromContext(ctx).WithName("operator-scrape-wiring")
	informer, err := r.Cache.GetInformer(ctx, &corev1.Secret{})
	if err != nil {
		log.Error(err, "unable to watch scrape-wiring Secrets; relying on rotation interval only")
		return
	}
	notify := func(obj any) {
		notifyScrapeWiringWake(namespace, wake, obj)
	}
	_, err = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    notify,
		UpdateFunc: func(_, newObj any) { notify(newObj) },
		DeleteFunc: notify,
	})
	if err != nil {
		log.Error(err, "unable to register scrape-wiring Secret handler; relying on rotation interval only")
	}
}

// notifyScrapeWiringWake signals wake when obj is a scrape-wiring Secret in namespace.
// Non-blocking: a full wake channel means a reconcile is already pending.
func notifyScrapeWiringWake(namespace string, wake chan struct{}, obj any) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		secret, ok = tombstone.Obj.(*corev1.Secret)
		if !ok {
			return
		}
	}
	if secret.GetNamespace() != namespace || !kubernetes.IsMetricsScrapeWiringSecret(secret) {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
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

	// Token first, then TLS gate, then ServiceMonitor — same ordering as operand reconcilers.
	result, err := common.ReconcilePrometheusScrapeToken(ctx, common.ScrapeTokenReconcilerConfig{
		Client:             r.Client,
		SecretReader:       r.SecretReader,
		Clock:              clk,
		TokenCreator:       r.TokenCreator,
		Scraper:            scraper,
		OperandNamespace:   namespace,
		ServiceMonitorName: operatorServiceMonitorName,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return kubernetes.ApplyScrapeTokenSecret(applyCtx, r.Client, secret)
		},
		ApplyServiceMonitor: func(applyCtx context.Context) error {
			return EnsureOperatorServiceMonitor(applyCtx, r.Client, namespace)
		},
	})
	if err != nil {
		return 0, err
	}
	return result.RequeueAfter, nil
}

func (r *ScrapeTokenRotator) rotationInterval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}
	return kubernetes.DefaultScrapeTokenRotationInterval
}
