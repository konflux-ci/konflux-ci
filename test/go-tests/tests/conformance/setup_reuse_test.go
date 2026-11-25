package conformance

import (
	"strings"
	"testing"

	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	imagecontroller "github.com/konflux-ci/image-controller/api/v1alpha1"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestLoadExistingReleaseSetup(t *testing.T) {
	const tenant, managed = "tenant", "managed"
	const name, app, component = "pre-created-plan", "test-app-anqa", "test-component-anqa"
	metadata := func(ns, name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Namespace: ns, Name: name}
	}
	for _, tt := range []struct {
		name        string
		application string
		origin      string
		existingApp bool
		missingPush bool
		wantError   string
	}{
		{name: "matching plan and prerequisites", application: app, origin: tenant},
		{name: "different test", application: "other-app-anqa", origin: tenant, wantError: "must match"},
		{name: "wrong admission origin", application: app, origin: "other", wantError: "must admit"},
		{name: "missing push secret", application: app, origin: tenant, missingPush: true, wantError: "needs a push Secret"},
		{name: "existing application", application: app, origin: tenant, existingApp: true, wantError: "already exists"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appstudioApi.AddToScheme, imagecontroller.AddToScheme, releaseApi.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			plan := &releaseApi.ReleasePlan{ObjectMeta: metadata(tenant, name)}
			plan.Labels = map[string]string{autoReleaseLabel: "true"}
			plan.Spec.Application, plan.Spec.Target = tt.application, managed
			admission := &releaseApi.ReleasePlanAdmission{ObjectMeta: metadata(managed, name)}
			admission.Labels = map[string]string{autoReleaseLabel: "true"}
			admission.Spec.Origin, admission.Spec.Applications, admission.Spec.Policy = tt.origin, []string{app}, "policy"
			sa := &corev1.ServiceAccount{ObjectMeta: metadata(managed, "release-pipeline")}
			objects := []crclient.Object{plan, admission, sa}
			for _, irName := range []string{component, "trusted-artifacts-anqa"} {
				ir := &imagecontroller.ImageRepository{ObjectMeta: metadata(managed, irName)}
				ir.Status.Credentials.PushSecretName = irName + "-push"
				if !tt.missingPush {
					sa.Secrets = append(sa.Secrets, corev1.ObjectReference{Name: ir.Status.Credentials.PushSecretName})
				}
				objects = append(objects, ir)
			}
			if tt.existingApp {
				objects = append(objects, &appstudioApi.Application{ObjectMeta: metadata(tenant, app)})
			}
			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			gotApp, gotComponent, result, err := loadExistingReleaseSetup(t.Context(), client, tenant, managed, name, "test-app", "test-component")
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
				}
				return
			}
			if err != nil || gotApp != app || gotComponent != component || result.ECPName != "policy" || result.TrustedArtifactsIR != "trusted-artifacts-anqa" {
				t.Fatalf("unexpected release setup: %q, %q, %+v, %v", gotApp, gotComponent, result, err)
			}
		})
	}
}
