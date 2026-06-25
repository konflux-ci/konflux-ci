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

	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// ScrapeTokenReconcilerConfig configures operand-namespace Prometheus scrape token reconciliation.
type ScrapeTokenReconcilerConfig struct {
	Client           client.Reader
	Clock            clock.Clock
	TokenCreator     kubernetes.TokenCreator
	ClusterInfo      *clusterinfo.Info
	OperandNamespace string
	Apply            kubernetes.ScrapeTokenApplyFunc
}

// ReconcilePrometheusScrapeToken ensures the operand scrape token Secret exists and is fresh.
func ReconcilePrometheusScrapeToken(ctx context.Context, cfg ScrapeTokenReconcilerConfig) (ctrl.Result, error) {
	if cfg.TokenCreator == nil {
		return ctrl.Result{}, fmt.Errorf("token creator is required")
	}
	if cfg.OperandNamespace == "" {
		return ctrl.Result{}, fmt.Errorf("operand namespace is required")
	}
	openShift := cfg.ClusterInfo != nil && cfg.ClusterInfo.IsOpenShift()
	requeue, err := kubernetes.EnsurePrometheusScrapeToken(ctx, kubernetes.EnsureScrapeTokenInput{
		Client:           cfg.Client,
		Clock:            cfg.Clock,
		TokenCreator:     cfg.TokenCreator,
		OpenShift:        openShift,
		OperandNamespace: cfg.OperandNamespace,
		Apply:            cfg.Apply,
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue > 0 {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	return ctrl.Result{}, nil
}

// MergeRequeueAfter keeps the shortest positive RequeueAfter between two results.
func MergeRequeueAfter(result, extra ctrl.Result) ctrl.Result {
	if extra.RequeueAfter <= 0 {
		return result
	}
	if result.RequeueAfter <= 0 || extra.RequeueAfter < result.RequeueAfter {
		result.RequeueAfter = extra.RequeueAfter
	}
	return result
}
