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
	"sort"

	rbacv1 "k8s.io/api/rbac/v1"
)

// AccessRequest describes a single RBAC authorization check against ClusterRole rules.
type AccessRequest struct {
	APIGroup string
	Resource string
	Verb     string
	Name     string
}

// RulesAllow reports whether any rule in rules permits req.
func RulesAllow(rules []rbacv1.PolicyRule, req AccessRequest) bool {
	for _, rule := range rules {
		if ruleAllows(rule, req) {
			return true
		}
	}
	return false
}

func ruleAllows(rule rbacv1.PolicyRule, req AccessRequest) bool {
	if !verbMatches(rule.Verbs, req.Verb) {
		return false
	}
	if !groupMatches(rule.APIGroups, req.APIGroup) {
		return false
	}
	if !resourceMatches(rule.Resources, req.Resource) {
		return false
	}
	if len(rule.ResourceNames) == 0 {
		return true
	}
	for _, name := range rule.ResourceNames {
		if name == req.Name {
			return true
		}
	}
	return false
}

func verbMatches(ruleVerbs []string, verb string) bool {
	for _, ruleVerb := range ruleVerbs {
		if ruleVerb == "*" || ruleVerb == verb {
			return true
		}
	}
	return false
}

func groupMatches(ruleGroups []string, group string) bool {
	for _, ruleGroup := range ruleGroups {
		if ruleGroup == "*" || ruleGroup == group {
			return true
		}
	}
	return false
}

func resourceMatches(ruleResources []string, resource string) bool {
	for _, ruleResource := range ruleResources {
		if ruleResource == "*" || ruleResource == resource {
			return true
		}
	}
	return false
}

// ResourceNamesWithVerb returns sorted unique resourceNames from rules that grant verb on resource.
func ResourceNamesWithVerb(rules []rbacv1.PolicyRule, apiGroup, resource, verb string) []string {
	seen := make(map[string]struct{})
	for _, rule := range rules {
		if len(rule.ResourceNames) == 0 {
			continue
		}
		if !verbMatches(rule.Verbs, verb) {
			continue
		}
		if !groupMatches(rule.APIGroups, apiGroup) {
			continue
		}
		if !resourceMatches(rule.Resources, resource) {
			continue
		}
		for _, name := range rule.ResourceNames {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// HasUnscopedVerb returns true when rules grant verb on resource without resourceNames.
func HasUnscopedVerb(rules []rbacv1.PolicyRule, apiGroup, resource, verb string) bool {
	req := AccessRequest{APIGroup: apiGroup, Resource: resource, Verb: verb, Name: "any-name"}
	for _, rule := range rules {
		if len(rule.ResourceNames) > 0 {
			continue
		}
		if ruleAllows(rule, req) {
			return true
		}
	}
	return false
}
