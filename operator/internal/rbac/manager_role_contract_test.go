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
	"os"
	"path/filepath"
	"runtime"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const rbacAuthzAPIGroup = "rbac.authorization.k8s.io"

// ClusterRoles granted escalate/bind on clusterroles in manager-role but not defined in component manifests.
var managerRoleClusterRoleEscalateBindExtras = []string{
	"konflux-operator-metrics-reader",
	"konflux-maintainer-user-actions", // aggregate role; default-tenant controller bind only
}

func loadManagerRoleRules(t *testing.T) []rbacv1.PolicyRule {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	rolePath := filepath.Join(filepath.Dir(filename), "..", "..", "config", "rbac", "role.yaml")

	data, err := os.ReadFile(rolePath)
	if err != nil {
		t.Fatalf("read manager role %s: %v", rolePath, err)
	}

	role := &rbacv1.ClusterRole{}
	if err := yaml.Unmarshal(data, role); err != nil {
		t.Fatalf("unmarshal manager role: %v", err)
	}
	if role.Name != "manager-role" {
		t.Fatalf("expected manager-role, got %q", role.Name)
	}
	return role.Rules
}

func TestManagerRoleForbidsUnscopedEscalateOnClusterRoles(t *testing.T) {
	rules := loadManagerRoleRules(t)
	if HasUnscopedVerb(rules, rbacAuthzAPIGroup, "clusterroles", "escalate") {
		t.Fatal("manager-role grants unscoped escalate on clusterroles (cluster-admin escalation path)")
	}
}

func TestManagerRoleForbidsUnscopedBindOnClusterRoleBindings(t *testing.T) {
	rules := loadManagerRoleRules(t)
	if HasUnscopedVerb(rules, rbacAuthzAPIGroup, "clusterrolebindings", "bind") {
		t.Fatal("manager-role grants unscoped bind on clusterrolebindings (cluster-admin escalation path)")
	}
}

func TestManagerRoleForbidsUnscopedBindOnClusterRoles(t *testing.T) {
	rules := loadManagerRoleRules(t)
	if HasUnscopedVerb(rules, rbacAuthzAPIGroup, "clusterroles", "bind") {
		t.Fatal("manager-role grants unscoped bind on clusterroles (cluster-admin escalation path)")
	}
}

func TestManagerRoleClusterRoleEscalateAndBindNamesAreKnown(t *testing.T) {
	rules := loadManagerRoleRules(t)
	embeddedByComponent, err := AllEmbeddedClusterRoles()
	if err != nil {
		t.Fatalf("AllEmbeddedClusterRoles: %v", err)
	}

	allowed := make(map[string]struct{}, len(managerRoleClusterRoleEscalateBindExtras))
	for _, name := range managerRoleClusterRoleEscalateBindExtras {
		allowed[name] = struct{}{}
	}
	for _, names := range embeddedByComponent {
		for _, name := range names {
			allowed[name] = struct{}{}
		}
	}

	for _, verb := range []string{"escalate", "bind"} {
		for _, name := range ResourceNamesWithVerb(rules, rbacAuthzAPIGroup, "clusterroles", verb) {
			if _, ok := allowed[name]; !ok {
				t.Fatalf("manager-role grants %s on unexpected clusterrole %q", verb, name)
			}
		}
	}
}

func TestManagerRoleAllowsEscalateOnEmbeddedClusterRoles(t *testing.T) {
	rules := loadManagerRoleRules(t)
	rolesByComponent, err := AllEmbeddedClusterRoles()
	if err != nil {
		t.Fatalf("AllEmbeddedClusterRoles: %v", err)
	}
	for component, roleNames := range rolesByComponent {
		assertEscalateAllowed(t, rules, component, roleNames)
	}
}

func assertEscalateAllowed(t *testing.T, rules []rbacv1.PolicyRule, component manifests.Component, roleNames []string) {
	t.Helper()
	for _, roleName := range roleNames {
		req := AccessRequest{
			APIGroup: rbacAuthzAPIGroup,
			Resource: "clusterroles",
			Verb:     "escalate",
			Name:     roleName,
		}
		if !RulesAllow(rules, req) {
			t.Errorf("component %s: manager-role must allow escalate on ClusterRole %q", component, roleName)
		}
	}
}

func TestManagerRoleAllowsCreateOnClusterRoles(t *testing.T) {
	rules := loadManagerRoleRules(t)
	req := AccessRequest{
		APIGroup: rbacAuthzAPIGroup,
		Resource: "clusterroles",
		Verb:     "create",
		Name:     "konflux-admin-user-actions-core",
	}
	if !RulesAllow(rules, req) {
		t.Fatal("manager-role must allow create on clusterroles for KonfluxRBAC reconciliation")
	}
}

func TestManagerRoleDeniesEscalateOnArbitraryClusterRole(t *testing.T) {
	rules := loadManagerRoleRules(t)
	req := AccessRequest{
		APIGroup: rbacAuthzAPIGroup,
		Resource: "clusterroles",
		Verb:     "escalate",
		Name:     "evil-cluster-admin",
	}
	if RulesAllow(rules, req) {
		t.Fatal("manager-role must not allow escalate on arbitrary clusterroles")
	}
}

func TestManagerRoleDeniesBindOnArbitraryClusterRoleBinding(t *testing.T) {
	rules := loadManagerRoleRules(t)
	req := AccessRequest{
		APIGroup: rbacAuthzAPIGroup,
		Resource: "clusterrolebindings",
		Verb:     "bind",
		Name:     "evil-cluster-admin-binding",
	}
	if RulesAllow(rules, req) {
		t.Fatal("manager-role must not allow bind on arbitrary clusterrolebindings")
	}
}
