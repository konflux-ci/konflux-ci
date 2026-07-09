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
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

func TestApplyMetricsScraperBindingSubjects(t *testing.T) {
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "prometheus-build-service-metrics-reader",
			Annotations: map[string]string{
				kubernetes.MetricsScraperBindingAnnotation: "true",
			},
		},
	}
	if err := ApplyMetricsScraperBindingSubjects(testBuildServiceNamespace, crb); err != nil {
		t.Fatalf("apply structured CRB: %v", err)
	}
	if len(crb.Subjects) != 1 {
		t.Fatalf("expected one subject, got %d", len(crb.Subjects))
	}
	if crb.Subjects[0].Name != kubernetes.MetricsScraperServiceAccountName {
		t.Fatalf("unexpected subject name %q", crb.Subjects[0].Name)
	}
	if crb.Subjects[0].Namespace != testBuildServiceNamespace {
		t.Fatalf("unexpected subject namespace %q", crb.Subjects[0].Namespace)
	}

	u := &unstructured.Unstructured{}
	u.SetAPIVersion("rbac.authorization.k8s.io/v1")
	u.SetKind("ClusterRoleBinding")
	u.SetName("prometheus-image-controller-metrics-reader")
	u.SetAnnotations(map[string]string{
		kubernetes.MetricsScraperBindingAnnotation: "true",
	})
	if err := ApplyMetricsScraperBindingSubjects(testImageControllerNamespace, u); err != nil {
		t.Fatalf("apply unstructured CRB: %v", err)
	}
	subjects, found, err := unstructured.NestedSlice(u.Object, "subjects")
	if err != nil || !found || len(subjects) != 1 {
		t.Fatalf("unexpected unstructured subjects: found=%v err=%v len=%d", found, err, len(subjects))
	}
}
