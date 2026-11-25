package conformance

import (
	"context"
	"fmt"
	"slices"
	"strings"

	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	imagecontroller "github.com/konflux-ci/image-controller/api/v1alpha1"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const autoReleaseLabel = "release.appstudio.openshift.io/auto-release"

// Reuse the Application suffix when constructing names required by the
// existing release image mappings.
func existingReleaseApplicationSuffix(base, application string) (string, error) {
	suffix, ok := strings.CutPrefix(application, base+"-")
	if !ok || len(validation.IsDNS1123Label(suffix)) != 0 {
		return "", fmt.Errorf("ReleasePlan application %q must match %s-<suffix>", application, base)
	}
	return suffix, nil
}

func loadExistingReleaseSetup(ctx context.Context, client crclient.Client, tenantNS, managedNS, releaseName, appBase, componentBase string) (string, string, setupReleaseResult, error) {
	var result setupReleaseResult
	plan := &releaseApi.ReleasePlan{}
	if err := client.Get(ctx, crclient.ObjectKey{Namespace: tenantNS, Name: releaseName}, plan); err != nil {
		return "", "", result, fmt.Errorf("read existing ReleasePlan %s/%s: %w", tenantNS, releaseName, err)
	}
	appName := plan.Spec.Application
	suffix, err := existingReleaseApplicationSuffix(appBase, appName)
	if err != nil {
		return "", "", result, err
	}
	if plan.Spec.Target != managedNS || plan.Labels[autoReleaseLabel] != "true" || plan.DeletionTimestamp != nil {
		return "", "", result, fmt.Errorf("ReleasePlan %s/%s must target %s, enable auto-release, and be active", tenantNS, releaseName, managedNS)
	}
	componentName := componentBase + "-" + suffix

	admission := &releaseApi.ReleasePlanAdmission{}
	if err := client.Get(ctx, crclient.ObjectKey{Namespace: managedNS, Name: releaseName}, admission); err != nil {
		return "", "", result, fmt.Errorf("read matching ReleasePlanAdmission %s/%s: %w", managedNS, releaseName, err)
	}
	if admission.Spec.Origin != tenantNS || !slices.Contains(admission.Spec.Applications, appName) ||
		admission.Spec.Policy == "" || admission.Labels[autoReleaseLabel] != "true" || admission.DeletionTimestamp != nil {
		return "", "", result, fmt.Errorf("ReleasePlanAdmission %s/%s must admit %s/%s, enable auto-release, reference a policy, and be active", managedNS, releaseName, tenantNS, appName)
	}
	result.ECPName = admission.Spec.Policy
	result.TrustedArtifactsIR = "trusted-artifacts-" + suffix

	// Keep reusable prerequisites read-only. The administrator provisions the
	// release pipeline account and mounts its push credentials in advance.
	sa := &corev1.ServiceAccount{}
	if err := client.Get(ctx, crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}, sa); err != nil {
		return "", "", result, fmt.Errorf("read release-pipeline ServiceAccount in %s: %w", managedNS, err)
	}
	for _, name := range []string{componentName, result.TrustedArtifactsIR} {
		ir := &imagecontroller.ImageRepository{}
		if err := client.Get(ctx, crclient.ObjectKey{Namespace: managedNS, Name: name}, ir); err != nil {
			return "", "", result, fmt.Errorf("read existing ImageRepository %s/%s: %w", managedNS, name, err)
		}
		secretName := ir.Status.Credentials.PushSecretName
		if secretName == "" || !slices.ContainsFunc(sa.Secrets, func(ref corev1.ObjectReference) bool { return ref.Name == secretName }) {
			return "", "", result, fmt.Errorf("ImageRepository %s/%s needs a push Secret listed on release-pipeline", managedNS, name)
		}
	}

	// Refuse to adopt or clean up an Application or Component from another run.
	for _, resource := range []struct {
		key    crclient.ObjectKey
		object crclient.Object
	}{
		{crclient.ObjectKey{Namespace: tenantNS, Name: appName}, &appstudioApi.Application{}},
		{crclient.ObjectKey{Namespace: tenantNS, Name: componentName}, &appstudioApi.Component{}},
	} {
		if err := client.Get(ctx, resource.key, resource.object); !k8sErrors.IsNotFound(err) {
			if err == nil {
				return "", "", result, fmt.Errorf("test resource %T %s already exists; use a dedicated plan or clean up the previous run", resource.object, resource.key)
			}
			return "", "", result, fmt.Errorf("check for existing test resource %T %s: %w", resource.object, resource.key, err)
		}
	}
	return appName, componentName, result, nil
}
