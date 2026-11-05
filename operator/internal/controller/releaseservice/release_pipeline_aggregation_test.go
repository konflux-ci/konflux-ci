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

package releaseservice

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const releasePipelineAggregateLabel = "rbac.konflux-ci.dev/aggregate-to-release-pipeline"

var _ = Describe("release pipeline ClusterRole aggregation", func() {
	It("keeps the aggregation shell out of the reconciled release manifest", func() {
		content, err := manifests.GetManifest(manifests.Release)
		Expect(err).NotTo(HaveOccurred())

		roles := clusterRolesFromYAML(content)
		Expect(roles).NotTo(HaveKey("release-pipeline-resource-role"))

		core, ok := roles["release-pipeline-resource-role-core"]
		Expect(ok).To(BeTrue())
		Expect(core.Labels).To(HaveKeyWithValue(releasePipelineAggregateLabel, "true"))
		Expect(core.AggregationRule).To(BeNil())
		expectPipelineCoreRules(core.Rules)
	})

	It("installs release-pipeline-resource-role as an aggregation shell", func() {
		shell := loadReleasePipelineShell()
		Expect(shell.Name).To(Equal("release-pipeline-resource-role"))
		Expect(shell.Annotations).To(HaveKeyWithValue("argocd.argoproj.io/compare-options", "IgnoreExtraneous"))
		Expect(shell.Rules).To(BeEmpty())
		Expect(shell.AggregationRule).NotTo(BeNil())
		Expect(shell.AggregationRule.ClusterRoleSelectors).To(ConsistOf(metav1.LabelSelector{
			MatchLabels: map[string]string{releasePipelineAggregateLabel: "true"},
		}))
	})

	It("selects the core fragment and any additional fragment with the same label", func() {
		shell := loadReleasePipelineShell()
		Expect(shell.AggregationRule.ClusterRoleSelectors).To(HaveLen(1))
		selector, err := metav1.LabelSelectorAsSelector(&shell.AggregationRule.ClusterRoleSelectors[0])
		Expect(err).NotTo(HaveOccurred())

		core := releasePipelineCoreFromManifest()
		Expect(selector.Matches(labels.Set(core.Labels))).To(BeTrue())
		Expect(selector.Matches(labels.Set{
			releasePipelineAggregateLabel: "true",
		})).To(BeTrue())
		Expect(selector.Matches(labels.Set{
			"rbac.konflux-ci.dev/aggregate-to-admin": "true",
		})).To(BeFalse())
	})
})

func clusterRolesFromYAML(content []byte) map[string]rbacv1.ClusterRole {
	GinkgoHelper()
	roles := map[string]rbacv1.ClusterRole{}
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	for {
		doc, err := reader.Read()
		if err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		role := rbacv1.ClusterRole{}
		Expect(yaml.Unmarshal(doc, &role)).To(Succeed())
		if role.Kind != "ClusterRole" || role.Name == "" {
			continue
		}
		roles[role.Name] = role
	}
	return roles
}

func loadReleasePipelineShell() *rbacv1.ClusterRole {
	GinkgoHelper()
	_, filename, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "config", "default", "release-rbac", "release_pipeline_resource_role.yaml")
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	shell := &rbacv1.ClusterRole{}
	Expect(yaml.Unmarshal(data, shell)).To(Succeed())
	return shell
}

func releasePipelineCoreFromManifest() *rbacv1.ClusterRole {
	GinkgoHelper()
	content, err := manifests.GetManifest(manifests.Release)
	Expect(err).NotTo(HaveOccurred())
	core, ok := clusterRolesFromYAML(content)["release-pipeline-resource-role-core"]
	Expect(ok).To(BeTrue())
	return core.DeepCopy()
}

func expectPipelineCoreRules(rules []rbacv1.PolicyRule) {
	GinkgoHelper()
	expectPipelineCoreRulesG(Default, rules)
}

func expectPipelineCoreRulesG(g Gomega, rules []rbacv1.PolicyRule) {
	g.Expect(rules).To(ContainElements(
		rbacv1.PolicyRule{
			APIGroups: []string{"appstudio.redhat.com"},
			Resources: []string{"releases", "releaseplans", "releaseplanadmissions", "snapshots"},
			Verbs:     []string{"get", "list", "watch"},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{"appstudio.redhat.com"},
			Resources: []string{"internalrequests"},
			Verbs:     []string{"create", "delete", "get", "list", "watch"},
		},
		rbacv1.PolicyRule{
			APIGroups: []string{"appstudio.redhat.com"},
			Resources: []string{"releases/status"},
			Verbs:     []string{"get", "patch"},
		},
	))
}
