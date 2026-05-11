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

package buildservice

import (
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	sigyaml "sigs.k8s.io/yaml"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// applyPipelineConfigMerge merges user-specified pipeline configuration into the
// build-pipeline-config ConfigMap. When pipelineConfig is nil, the defaults are
// used unchanged. If the effective default pipeline is missing from the merged
// list (e.g. user removed it without setting a new one), the first available
// pipeline is promoted to default and a warning is logged.
func applyPipelineConfigMerge(log logr.Logger, configMap *corev1.ConfigMap, pipelineConfig *konfluxv1alpha1.PipelineConfigSpec) error {
	if pipelineConfig == nil {
		return nil
	}

	configData, ok := configMap.Data["config.yaml"]
	if !ok {
		return fmt.Errorf("build-pipeline-config ConfigMap missing config.yaml key")
	}

	var cfg konfluxv1alpha1.PipelineConfigData
	if err := sigyaml.Unmarshal([]byte(configData), &cfg); err != nil {
		return fmt.Errorf("failed to parse config.yaml: %w", err)
	}

	cfg.Pipelines = mergePipelines(cfg.Pipelines, pipelineConfig)

	if pipelineConfig.DefaultPipelineName != "" {
		cfg.DefaultPipelineName = pipelineConfig.DefaultPipelineName
	}

	// If the effective default pipeline is no longer in the merged list,
	// auto-select the first available pipeline. Most invalid configurations
	// are caught by CRD CEL rules, but this handles the case where the user
	// removes the operator-provided default without setting a replacement.
	ensureDefaultPipeline(log, &cfg)

	out, err := sigyaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize merged config.yaml: %w", err)
	}

	configMap.Data["config.yaml"] = string(out)
	return nil
}

// mergePipelines applies the merge logic:
//  1. If removeDefaults is true, start with an empty list
//  2. Otherwise start with the defaults
//  3. For each user pipeline: if removed, delete from list; otherwise upsert
func mergePipelines(defaults []konfluxv1alpha1.PipelineEntryData, spec *konfluxv1alpha1.PipelineConfigSpec) []konfluxv1alpha1.PipelineEntryData {
	var result []konfluxv1alpha1.PipelineEntryData
	if !spec.RemoveDefaults {
		result = make([]konfluxv1alpha1.PipelineEntryData, len(defaults))
		copy(result, defaults)
	} else {
		result = []konfluxv1alpha1.PipelineEntryData{}
	}

	for _, p := range spec.Pipelines {
		if p.Removed {
			filtered := make([]konfluxv1alpha1.PipelineEntryData, 0, len(result))
			for _, r := range result {
				if r.Name != p.Name {
					filtered = append(filtered, r)
				}
			}
			result = filtered
			continue
		}

		// Upsert: replace existing or append.
		// When overriding an existing entry, preserve the default description
		// unless the user supplied their own.
		found := false
		for i, r := range result {
			if r.Name == p.Name {
				desc := p.Description
				if desc == "" {
					desc = r.Description
				}
				result[i] = konfluxv1alpha1.PipelineEntryData{Name: p.Name, Bundle: p.Bundle, Description: desc}
				found = true
				break
			}
		}
		if !found {
			result = append(result, konfluxv1alpha1.PipelineEntryData{Name: p.Name, Bundle: p.Bundle, Description: p.Description})
		}
	}

	return result
}

// ensureDefaultPipeline verifies that the default pipeline exists in the
// pipelines list. If it does not, the first available pipeline is promoted
// and a warning is logged. This is a graceful fallback for edge cases that
// cannot be caught by CRD-level CEL validation (e.g. removing the
// operator-provided default without setting a replacement).
func ensureDefaultPipeline(log logr.Logger, cfg *konfluxv1alpha1.PipelineConfigData) {
	if cfg.DefaultPipelineName == "" || len(cfg.Pipelines) == 0 {
		return
	}

	for _, p := range cfg.Pipelines {
		if p.Name == cfg.DefaultPipelineName {
			return
		}
	}

	previous := cfg.DefaultPipelineName
	cfg.DefaultPipelineName = cfg.Pipelines[0].Name
	log.Info("Default pipeline was removed from merged list; auto-selected first available pipeline",
		"previous", previous, "selected", cfg.DefaultPipelineName)
}
