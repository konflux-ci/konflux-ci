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

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// ScrapeTokenReconcilerConfig configures operand-namespace Prometheus scrape token reconciliation.
type ScrapeTokenReconcilerConfig struct {
	Client           client.Reader
	Clock            clock.Clock
	TokenCreator     kubernetes.TokenCreator
	Scraper          types.NamespacedName
	OperandNamespace string
	Apply            kubernetes.ScrapeTokenApplyFunc
}

// ReconcilePrometheusScrapeToken ensures the operand scrape token Secret exists and is fresh.
// Periodic re-checks are driven by TokenRotationBroadcaster rather than RequeueAfter.
func ReconcilePrometheusScrapeToken(ctx context.Context, cfg ScrapeTokenReconcilerConfig) error {
	if cfg.TokenCreator == nil {
		return fmt.Errorf("token creator is required")
	}
	if cfg.OperandNamespace == "" {
		return fmt.Errorf("operand namespace is required")
	}
	if cfg.Scraper.Namespace == "" || cfg.Scraper.Name == "" {
		return fmt.Errorf("scraper service account is required")
	}
	_, err := kubernetes.EnsurePrometheusScrapeToken(ctx, kubernetes.EnsureScrapeTokenInput{
		Client:           cfg.Client,
		Clock:            cfg.Clock,
		TokenCreator:     cfg.TokenCreator,
		Scraper:          cfg.Scraper,
		OperandNamespace: cfg.OperandNamespace,
		Apply:            cfg.Apply,
	})
	return err
}
