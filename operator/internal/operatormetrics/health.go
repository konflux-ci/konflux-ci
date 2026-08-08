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
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var konfluxUp = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "konflux_up",
	Help: "Whether the Konflux operator is ready (1) or not (0). Aggregated from all component sub-CR readiness conditions.",
	ConstLabels: prometheus.Labels{
		"service": "konflux-operator",
		"check":   "operator-ready",
	},
})

func init() {
	ctrlmetrics.Registry.MustRegister(konfluxUp)
}

// SetKonfluxUp updates the konflux_up gauge based on the Konflux CR readiness.
func SetKonfluxUp(ready bool) {
	if ready {
		konfluxUp.Set(1)
	} else {
		konfluxUp.Set(0)
	}
}

// KonfluxUpGauge returns the konflux_up gauge collector for use in tests.
func KonfluxUpGauge() prometheus.Gauge {
	return konfluxUp
}
