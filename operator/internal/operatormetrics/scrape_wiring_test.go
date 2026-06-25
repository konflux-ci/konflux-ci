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
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

func TestEnsureOperatorServiceMonitorCreatesAndUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := fake.NewClientBuilder().Build()

	if err := EnsureOperatorServiceMonitor(ctx, c, OperatorNamespace); err != nil {
		t.Fatalf("ensure ServiceMonitor: %v", err)
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{
		Name:      operatorServiceMonitorName,
		Namespace: OperatorNamespace,
	}, sm); err != nil {
		t.Fatalf("get ServiceMonitor: %v", err)
	}
	if sm.GetLabels()["app.kubernetes.io/managed-by"] != OperatorAppName {
		t.Fatalf("unexpected labels: %#v", sm.GetLabels())
	}
	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 {
		t.Fatalf("unexpected endpoints: found=%v err=%v len=%d", found, err, len(endpoints))
	}
	endpoint, ok := endpoints[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected endpoint map")
	}
	tokenSecret, ok := endpoint["bearerTokenSecret"].(map[string]interface{})
	if !ok || tokenSecret["name"] != kubernetes.ScrapeTokenSecretName {
		t.Fatalf("unexpected bearer token secret: %#v", endpoint["bearerTokenSecret"])
	}

	if err := EnsureOperatorServiceMonitor(ctx, c, OperatorNamespace); err != nil {
		t.Fatalf("re-ensure ServiceMonitor: %v", err)
	}
}

func TestEnsureOperatorServiceMonitorRequiresNamespace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := fake.NewClientBuilder().Build()

	if err := EnsureOperatorServiceMonitor(ctx, c, ""); err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

func TestDesiredOperatorServiceMonitorSpec(t *testing.T) {
	t.Parallel()
	sm := desiredOperatorServiceMonitor(OperatorNamespace)
	if sm.GetNamespace() != OperatorNamespace {
		t.Fatalf("unexpected namespace: %q", sm.GetNamespace())
	}
	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 {
		t.Fatalf("unexpected endpoints: found=%v err=%v len=%d", found, err, len(endpoints))
	}
	endpoint, ok := endpoints[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected endpoint map")
	}
	if endpoint["scheme"] != "https" {
		t.Fatalf("unexpected scheme: %#v", endpoint["scheme"])
	}
}
