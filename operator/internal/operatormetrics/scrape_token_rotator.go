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

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

const (
	// OperatorNamespace is the deployment namespace for the Konflux operator manager.
	OperatorNamespace = "konflux-operator"
)

// ScrapeTokenRotator keeps prometheus-scrape-token fresh in the operator namespace.
type ScrapeTokenRotator struct {
	Client       client.Client
	Clock        clock.Clock
	TokenCreator kubernetes.TokenCreator
	ClusterInfo  *clusterinfo.Info
	Namespace    string
}

// NeedLeaderElection ensures only the elected manager replica rotates the scrape token.
func (r *ScrapeTokenRotator) NeedLeaderElection() bool {
	return true
}

// Start runs until ctx is cancelled, requeueing based on token freshness.
func (r *ScrapeTokenRotator) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("operator-scrape-token")

	namespace := r.Namespace
	if namespace == "" {
		namespace = OperatorNamespace
	}

	for {
		requeue, err := r.reconcile(ctx, namespace)
		if err != nil {
			log.Error(err, "failed to reconcile operator prometheus scrape token")
			requeue = kubernetes.DefaultScrapeTokenMinRequeue
		}

		timer := time.NewTimer(requeue)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (r *ScrapeTokenRotator) reconcile(ctx context.Context, namespace string) (time.Duration, error) {
	clk := r.Clock
	if clk == nil {
		clk = clock.RealClock{}
	}
	openShift := r.ClusterInfo != nil && r.ClusterInfo.IsOpenShift()
	return kubernetes.EnsurePrometheusScrapeToken(ctx, kubernetes.EnsureScrapeTokenInput{
		Client:           r.Client,
		Clock:            clk,
		TokenCreator:     r.TokenCreator,
		OpenShift:        openShift,
		OperandNamespace: namespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return kubernetes.ApplyScrapeTokenSecret(applyCtx, r.Client, secret)
		},
	})
}
