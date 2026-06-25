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

package v1alpha1

// NewKonfluxBuildServiceSpec builds a KonfluxBuildService spec from user config and forwarded metrics.
func NewKonfluxBuildServiceSpec(cfg KonfluxBuildServiceConfigSpec, metrics *ComponentMetricsConfig) KonfluxBuildServiceSpec {
	return KonfluxBuildServiceSpec{
		KonfluxBuildServiceConfigSpec: cfg,
		ComponentMetrics:              metrics,
	}
}

// NewKonfluxImageControllerSpec builds a KonfluxImageController spec from user config and forwarded metrics.
func NewKonfluxImageControllerSpec(cfg KonfluxImageControllerConfigSpec, metrics *ComponentMetricsConfig) KonfluxImageControllerSpec {
	return KonfluxImageControllerSpec{
		KonfluxImageControllerConfigSpec: cfg,
		ComponentMetrics:                 metrics,
	}
}

// NewKonfluxIntegrationServiceSpec builds a KonfluxIntegrationService spec from user config and forwarded metrics.
func NewKonfluxIntegrationServiceSpec(cfg KonfluxIntegrationServiceConfigSpec, metrics *ComponentMetricsConfig) KonfluxIntegrationServiceSpec {
	return KonfluxIntegrationServiceSpec{
		KonfluxIntegrationServiceConfigSpec: cfg,
		ComponentMetrics:                    metrics,
	}
}
