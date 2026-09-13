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

package rbac

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// EmbeddedClusterRoles returns ClusterRole names from a component's embedded manifest.
func EmbeddedClusterRoles(component manifests.Component) ([]string, error) {
	content, err := manifests.GetManifest(component)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest for %s: %w", component, err)
	}

	docs, err := splitYAMLDocuments(content)
	if err != nil {
		return nil, fmt.Errorf("failed to split %s manifest: %w", component, err)
	}

	var names []string
	for _, doc := range docs {
		role := &rbacv1.ClusterRole{}
		if err := yaml.Unmarshal(doc, role); err != nil {
			return nil, fmt.Errorf("failed to unmarshal ClusterRole from %s manifest: %w", component, err)
		}
		if role.Kind != "ClusterRole" || role.Name == "" {
			continue
		}
		names = append(names, role.Name)
	}
	sort.Strings(names)
	return names, nil
}

// AllEmbeddedClusterRoles returns ClusterRole names grouped by component manifest.
func AllEmbeddedClusterRoles() (map[manifests.Component][]string, error) {
	result := make(map[manifests.Component][]string)
	for _, component := range manifests.AllComponents() {
		names, err := EmbeddedClusterRoles(component)
		if err != nil {
			return nil, err
		}
		if len(names) > 0 {
			result[component] = names
		}
	}
	return result, nil
}

func splitYAMLDocuments(content []byte) ([][]byte, error) {
	var docs [][]byte
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}
