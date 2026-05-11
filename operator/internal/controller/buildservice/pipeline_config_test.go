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
	"testing"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

func makeConfigMap() *corev1.ConfigMap {
	return &corev1.ConfigMap{
		Data: map[string]string{
			"config.yaml": defaultConfigYAML,
		},
	}
}

const defaultConfigYAML = `default-pipeline-name: docker-build-oci-ta
pipelines:
- name: fbc-builder
  bundle: quay.io/konflux-ci/tekton-catalog/pipeline-fbc-builder@sha256:abc123
- name: docker-build-oci-ta
  bundle: quay.io/konflux-ci/tekton-catalog/pipeline-docker-build-oci-ta@sha256:def456
  description: full docker build pipeline with trusted artifacts
`

func testLogger() logr.Logger {
	return logr.Discard()
}

func TestApplyPipelineConfigMerge(t *testing.T) {
	t.Run("nil pipelineConfig leaves defaults unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.Equal(defaultConfigYAML))
	})

	t.Run("empty pipelineConfig leaves defaults unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("removeDefaults with custom pipelines only", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults:      true,
			DefaultPipelineName: "my-custom",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/pipeline:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("- name: fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("- name: docker-build-oci-ta"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-custom"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("quay.io/myorg/pipeline:latest"))
	})

	t.Run("removeDefaults with no pipelines keeps dangling default (CEL should block this)", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults:      true,
			DefaultPipelineName: "my-only",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-only", Bundle: "quay.io/myorg/pipeline:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-only"))
	})

	t.Run("individual pipeline removal via removed: true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("removing nonexistent pipeline is a no-op", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "nonexistent", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("pipeline override preserves description from defaults", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "docker-build-oci-ta", Bundle: "quay.io/myorg/docker:v2"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("quay.io/myorg/docker:v2"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("description: full docker build pipeline with trusted artifacts"))
	})

	t.Run("pipeline override replaces bundle", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Bundle: "quay.io/myorg/fbc:v2"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("quay.io/myorg/fbc:v2"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("sha256:abc123"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("adding new pipelines alongside defaults", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/pipeline:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("docker-build-oci-ta"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-custom"))
	})

	t.Run("preserves default-pipeline-name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})

	t.Run("missing config.yaml key returns error", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{Data: map[string]string{}}
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{})
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("missing config.yaml"))
	})

	t.Run("defaultPipelineName override to existing default pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "fbc-builder",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("fbc-builder"))
	})

	t.Run("defaultPipelineName override to custom pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "my-custom",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: my-custom"))
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("my-custom"))
	})

	t.Run("defaultPipelineName with removeDefaults: true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults:      true,
			DefaultPipelineName: "my-only-pipeline",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-only-pipeline", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: my-only-pipeline"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("fbc-builder"))
		g.Expect(cm.Data["config.yaml"]).NotTo(gomega.ContainSubstring("docker-build-oci-ta"))
	})

	t.Run("auto-selects first pipeline when defaultPipelineName doesn't exist", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "nonexistent-pipeline",
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: fbc-builder"))
	})

	t.Run("auto-selects first pipeline when defaultPipelineName references a removed pipeline", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			DefaultPipelineName: "fbc-builder",
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "fbc-builder", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})

	t.Run("auto-selects first pipeline when current default is removed without setting new defaultPipelineName", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "docker-build-oci-ta", Removed: true},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: fbc-builder"))
	})

	t.Run("preserves existing default when defaultPipelineName field not set", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := makeConfigMap()
		err := applyPipelineConfigMerge(testLogger(), cm, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "my-custom", Bundle: "quay.io/myorg/custom:latest"},
			},
		})
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(cm.Data["config.yaml"]).To(gomega.ContainSubstring("default-pipeline-name: docker-build-oci-ta"))
	})
}

func TestMergePipelines(t *testing.T) {
	defaults := []konfluxv1alpha1.PipelineEntryData{
		{Name: "pipeline-a", Bundle: "bundle-a"},
		{Name: "pipeline-b", Bundle: "bundle-b", Description: "pipeline B description"},
		{Name: "pipeline-c", Bundle: "bundle-c"},
	}

	t.Run("does not modify defaults slice", func(t *testing.T) {
		g := gomega.NewWithT(t)
		originalLen := len(defaults)
		_ = mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-a", Removed: true},
			},
		})
		g.Expect(defaults).To(gomega.HaveLen(originalLen))
	})

	t.Run("removeDefaults ignores defaults", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			RemoveDefaults: true,
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "custom", Bundle: "custom-bundle"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(1))
		g.Expect(result[0].Name).To(gomega.Equal("custom"))
	})

	t.Run("upsert replaces by name and preserves description", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-b", Bundle: "new-bundle-b"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(3))
		for _, p := range result {
			if p.Name == "pipeline-b" {
				g.Expect(p.Bundle).To(gomega.Equal("new-bundle-b"))
				g.Expect(p.Description).To(gomega.Equal("pipeline B description"))
			}
		}
	})

	t.Run("user-provided description overrides default description", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-b", Bundle: "new-bundle-b", Description: "custom description"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(3))
		for _, p := range result {
			if p.Name == "pipeline-b" {
				g.Expect(p.Bundle).To(gomega.Equal("new-bundle-b"))
				g.Expect(p.Description).To(gomega.Equal("custom description"))
			}
		}
	})

	t.Run("new pipeline gets user-provided description", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-d", Bundle: "bundle-d", Description: "brand new pipeline"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(4))
		for _, p := range result {
			if p.Name == "pipeline-d" {
				g.Expect(p.Description).To(gomega.Equal("brand new pipeline"))
			}
		}
	})

	t.Run("remove then add same name", func(t *testing.T) {
		g := gomega.NewWithT(t)
		result := mergePipelines(defaults, &konfluxv1alpha1.PipelineConfigSpec{
			Pipelines: []konfluxv1alpha1.PipelineSpec{
				{Name: "pipeline-a", Removed: true},
				{Name: "pipeline-a", Bundle: "replacement"},
			},
		})
		g.Expect(result).To(gomega.HaveLen(3))
		found := false
		for _, p := range result {
			if p.Name == "pipeline-a" {
				g.Expect(p.Bundle).To(gomega.Equal("replacement"))
				found = true
			}
		}
		g.Expect(found).To(gomega.BeTrue())
	})
}

func TestEnsureDefaultPipeline(t *testing.T) {
	t.Run("no-op when default pipeline name is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &konfluxv1alpha1.PipelineConfigData{
			DefaultPipelineName: "",
			Pipelines: []konfluxv1alpha1.PipelineEntryData{
				{Name: "pipeline-a", Bundle: "bundle-a"},
			},
		}
		ensureDefaultPipeline(testLogger(), cfg)
		g.Expect(cfg.DefaultPipelineName).To(gomega.Equal(""))
	})

	t.Run("no-op when default pipeline exists in pipelines list", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &konfluxv1alpha1.PipelineConfigData{
			DefaultPipelineName: "pipeline-b",
			Pipelines: []konfluxv1alpha1.PipelineEntryData{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
				{Name: "pipeline-c", Bundle: "bundle-c"},
			},
		}
		ensureDefaultPipeline(testLogger(), cfg)
		g.Expect(cfg.DefaultPipelineName).To(gomega.Equal("pipeline-b"))
	})

	t.Run("auto-selects first pipeline when default is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &konfluxv1alpha1.PipelineConfigData{
			DefaultPipelineName: "nonexistent-pipeline",
			Pipelines: []konfluxv1alpha1.PipelineEntryData{
				{Name: "pipeline-a", Bundle: "bundle-a"},
				{Name: "pipeline-b", Bundle: "bundle-b"},
			},
		}
		ensureDefaultPipeline(testLogger(), cfg)
		g.Expect(cfg.DefaultPipelineName).To(gomega.Equal("pipeline-a"))
	})

	t.Run("no-op when default is set but no pipelines available", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cfg := &konfluxv1alpha1.PipelineConfigData{
			DefaultPipelineName: "some-pipeline",
			Pipelines:           []konfluxv1alpha1.PipelineEntryData{},
		}
		ensureDefaultPipeline(testLogger(), cfg)
		g.Expect(cfg.DefaultPipelineName).To(gomega.Equal("some-pipeline"))
	})
}
