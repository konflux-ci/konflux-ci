package conformance

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ecp "github.com/conforma/crds/api/v1alpha1"
	buildcontrollers "github.com/konflux-ci/build-service/controllers"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/framework"
	ginkgo "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// isPreProvisioned returns true when the suite is targeting a pre-provisioned
// (e.g. staging) cluster where the managed namespace is externally managed.
// The single source of truth is the E2E_MANAGED_NAMESPACE env var.
func isPreProvisioned() bool {
	return os.Getenv("E2E_MANAGED_NAMESPACE") != ""
}

// runSetupRelease downloads setup-release.sh from the ConfigMap shipped by the
// operator (konflux-cli/setup-release) and executes it to create the managed
// namespace, ImageRepositories, EnterpriseContractPolicy, ReleasePlanAdmission,
// and ReleasePlan needed by the release flow.
func runSetupRelease(appName, componentName, tenantNS, managedNS, releaseName string) error {
	scriptContent, err := downloadScriptFromConfigMap("konflux-cli", "setup-release", "setup-release.sh")
	if err != nil {
		return fmt.Errorf("download setup-release.sh from ConfigMap: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "setup-release-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	preProvisioned := isPreProvisioned()
	if preProvisioned {
		klog.Infof("conformance: managed namespace %s is pre-provisioned (E2E_MANAGED_NAMESPACE set), adapting setup-release.sh", managedNS)

		origLen := len(scriptContent)

		// Strip the namespace creation heredoc -- the namespace is managed
		// externally (e.g. GitOps) and the user may lack cluster-scope permissions.
		nsApply := regexp.MustCompile(`(?s)kubectl apply -f - <<EOF\napiVersion: v1\nkind: Namespace\n.*?\nEOF`)
		scriptContent = nsApply.ReplaceAll(scriptContent,
			[]byte(`echo "Namespace ${MANAGED_NS} already exists, skipping creation"`))

		// Wrap only the RoleBinding creation commands with || true so the script
		// continues past RBAC errors that are expected in pre-provisioned clusters
		// (where RBAC is managed by GitOps). Keep errexit active for all other
		// commands so real failures surface immediately.
		rbRe := regexp.MustCompile(`(?m)(kubectl apply -f - <<EOF\napiVersion: rbac\.authorization\.k8s\.io/v1\nkind: RoleBinding)`)
		scriptContent = rbRe.ReplaceAll(scriptContent,
			[]byte("kubectl apply -f - <<EOF || true\napiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding"))

		ssoRe := regexp.MustCompile("(\\{.data.\\$SSO_ACCOUNT\\}\")" + `\)`)
		scriptContent = ssoRe.ReplaceAll(scriptContent,
			[]byte("${1} 2>/dev/null || true)"))

		if len(scriptContent) == origLen {
			klog.Warningf("conformance: regex adaptations did not modify setup-release.sh; upstream script format may have changed")
		}
	}

	scriptPath := filepath.Join(tmpDir, "setup-release.sh")
	if err := os.WriteFile(scriptPath, scriptContent, 0o755); err != nil {
		return fmt.Errorf("write setup-release.sh: %w", err)
	}

	args := []string{
		"-t", tenantNS,
		"-m", managedNS,
		"-a", appName,
		"-c", componentName,
		"-r", releaseName,
	}
	klog.Infof("conformance: running setup-release.sh %v (from ConfigMap konflux-cli/setup-release)", args)
	cmd := exec.Command(scriptPath, args...)
	cmd.Stdout = ginkgo.GinkgoWriter
	cmd.Stderr = ginkgo.GinkgoWriter
	if err := cmd.Run(); err != nil {
		if preProvisioned {
			klog.Warningf("conformance: setup-release.sh exited with error (non-fatal in pre-provisioned env): %v", err)
		} else {
			return fmt.Errorf("setup-release.sh: %w", err)
		}
	}
	return nil
}

// e2eECPExclusions lists policy rules to exclude during E2E tests. Conformance runs
// docker-build-oci-ta-min with security tasks; exclude EC rules that are not required
// for this environment or that reference tasks outside the minimal pipeline bundle.
var e2eECPExclusions = []string{
	"cve",
	"tasks.required_tasks_found:roxctl-scan",
	"tasks.required_tasks_found:clamav-scan",
	"tasks.required_tasks_found:tpa-scan",
	"tasks.required_tasks_found:deprecated-image-check",
	"tasks.required_tasks_found:rpms-signature-scan",
	"tasks.required_tasks_found:sast-shell-check",
	"tasks.required_tasks_found:sast-shell-check-oci-ta",
	"tasks.required_tasks_found:sast-unicode-check",
	"tasks.required_tasks_found:sast-unicode-check-oci-ta",
	"test.test_data_found",
}

// patchECPForE2E appends E2E-specific exclusions to the EnterpriseContractPolicy
// in the managed namespace.
func patchECPForE2E(hub *framework.ControllerHub, policyName, managedNS string) error {
	klog.Infof("conformance: patching ECP %s/%s with E2E exclusions", managedNS, policyName)

	policy, err := hub.TektonController.GetEnterpriseContractPolicy(policyName, managedNS)
	if err != nil {
		return fmt.Errorf("get ECP %s/%s: %w", managedNS, policyName, err)
	}

	for i := range policy.Spec.Sources {
		if policy.Spec.Sources[i].Config == nil {
			policy.Spec.Sources[i].Config = &ecp.SourceConfig{}
		}
		seen := make(map[string]bool, len(policy.Spec.Sources[i].Config.Exclude))
		for _, e := range policy.Spec.Sources[i].Config.Exclude {
			seen[e] = true
		}
		for _, e := range e2eECPExclusions {
			if !seen[e] {
				policy.Spec.Sources[i].Config.Exclude = append(policy.Spec.Sources[i].Config.Exclude, e)
			}
		}
	}

	return hub.TektonController.KubeRest().Update(context.Background(), policy)
}

// resolveKubectl returns the path to kubectl or oc, preferring kubectl.
// Go's exec.Command performs a direct binary lookup and cannot see the bash
// function alias that run-e2e.sh sets up, so we need an explicit fallback.
func resolveKubectl() string {
	if p, err := exec.LookPath("kubectl"); err == nil {
		return p
	}
	if p, err := exec.LookPath("oc"); err == nil {
		return p
	}
	return "kubectl"
}

// downloadScriptFromConfigMap extracts a script from a ConfigMap using kubectl.
func downloadScriptFromConfigMap(namespace, configMapName, key string) ([]byte, error) {
	jsonpath := fmt.Sprintf("{.data.%s}", strings.ReplaceAll(key, ".", "\\."))
	cmd := exec.Command(resolveKubectl(), "get", "configmap", configMapName,
		"-n", namespace,
		"-o", fmt.Sprintf("jsonpath=%s", jsonpath))
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("kubectl get configmap %s/%s: %s", namespace, configMapName, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("kubectl get configmap %s/%s: %w", namespace, configMapName, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ConfigMap %s/%s key %q is empty", namespace, configMapName, key)
	}
	return out, nil
}

// grantIntegrationRunnerJobRBAC creates a Role + RoleBinding so that the
// konflux-integration-runner SA can manage Jobs and Pods in the tenant namespace.
func grantIntegrationRunnerJobRBAC(hub *framework.ControllerHub, namespace string) error {
	ctx := context.Background()
	client := hub.CommonController.KubeRest()

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "integration-runner-jobs",
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods/log"},
				Verbs:     []string{"get", "list"},
			},
			{
				APIGroups: []string{"batch"},
				Resources: []string{"jobs"},
				Verbs:     []string{"create", "delete", "get", "list", "watch"},
			},
		},
	}

	existing := &rbacv1.Role{}
	err := client.Get(ctx, crclient.ObjectKeyFromObject(role), existing)
	if err != nil {
		if !k8sErrors.IsNotFound(err) {
			return fmt.Errorf("get Role %s/%s: %w", namespace, role.Name, err)
		}
		if err := client.Create(ctx, role); err != nil {
			return fmt.Errorf("create Role %s/%s: %w", namespace, role.Name, err)
		}
	} else {
		existing.Rules = role.Rules
		if err := client.Update(ctx, existing); err != nil {
			return fmt.Errorf("update Role %s/%s: %w", namespace, role.Name, err)
		}
	}

	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "integration-runner-jobs",
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "integration-runner-jobs",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "konflux-integration-runner",
				Namespace: namespace,
			},
		},
	}

	existingRB := &rbacv1.RoleBinding{}
	err = client.Get(ctx, crclient.ObjectKeyFromObject(rb), existingRB)
	if err != nil {
		if !k8sErrors.IsNotFound(err) {
			return fmt.Errorf("get RoleBinding %s/%s: %w", namespace, rb.Name, err)
		}
		if err := client.Create(ctx, rb); err != nil {
			return fmt.Errorf("create RoleBinding %s/%s: %w", namespace, rb.Name, err)
		}
	}
	// RoleBinding roleRef is immutable; if it already exists with the right roleRef, no update needed.

	return nil
}

// dumpDiagnostics logs component build status, application status, PaC repository state,
// and PipelineRun inventory using klog (not GinkgoWriter, which is suppressed during Eventually retries).
func dumpDiagnostics(hub *framework.ControllerHub, componentName, appName, namespace string) {
	if comp, err := hub.HasController.GetComponent(componentName, namespace); err != nil {
		klog.Errorf("diagnostic: could not re-fetch Component %s/%s: %v", namespace, componentName, err)
	} else {
		msgs, _ := hub.HasController.GetComponentConditionStatusMessages(comp.GetName(), namespace)
		buildAnnot := comp.Annotations[buildcontrollers.BuildStatusAnnotationName]
		klog.Infof("diagnostic: Component %s/%s conditions=%v build-status=%q", namespace, comp.GetName(), msgs, buildAnnot)
	}

	if app, err := hub.HasController.GetApplication(appName, namespace); err != nil {
		klog.Errorf("diagnostic: could not get Application %s/%s: %v", namespace, appName, err)
	} else if len(app.Status.Conditions) > 0 {
		klog.Infof("diagnostic: Application %s/%s conditions=%+v", namespace, app.Name, app.Status.Conditions)
	} else {
		klog.Infof("diagnostic: Application %s/%s has no status conditions", namespace, app.Name)
	}

	if prs, err := hub.TektonController.ListAllPipelineRuns(namespace); err != nil {
		klog.Errorf("diagnostic: could not list PipelineRuns in %s: %v", namespace, err)
	} else {
		klog.Infof("diagnostic: PipelineRuns in %s: %d", namespace, len(prs.Items))
		for _, pr := range prs.Items {
			status := "Pending"
			for _, c := range pr.Status.Conditions {
				status = fmt.Sprintf("%s (reason: %s)", c.Status, c.Reason)
			}
			klog.Infof("diagnostic:   - %s sha=%s type=%s status=%s",
				pr.Name,
				pr.Labels["pipelinesascode.tekton.dev/sha"],
				pr.Labels["pipelinesascode.tekton.dev/event-type"],
				status)
		}
	}
}

// cleanupManagedResources deletes test-created resources from the managed namespace
// on pre-provisioned clusters where we cannot delete the namespace itself.
func cleanupManagedResources(fw *framework.Framework, ns string) {
	client := fw.AsKubeAdmin.CommonController.KubeRest()
	ctx := context.Background()

	if err := fw.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(ns); err != nil {
		klog.Warningf("conformance cleanup: delete PipelineRuns in %s: %v", ns, err)
	}
	if err := fw.AsKubeAdmin.HasController.DeleteAllImageRepositoriesInASpecificNamespace(ns, cleanupResourceTimeout); err != nil {
		klog.Warningf("conformance cleanup: delete ImageRepositories in %s: %v", ns, err)
	}
	if err := fw.AsKubeAdmin.TektonController.DeleteEnterpriseContractPolicy("default", ns, false); err != nil {
		klog.Warningf("conformance cleanup: delete ECP in %s: %v", ns, err)
	}
	if err := client.DeleteAllOf(ctx, &releaseApi.ReleasePlanAdmission{}, crclient.InNamespace(ns)); err != nil {
		klog.Warningf("conformance cleanup: delete ReleasePlanAdmissions in %s: %v", ns, err)
	}
}

// cleanupTenantResources deletes test-created resources from the tenant namespace
// on pre-provisioned clusters.
func cleanupTenantResources(fw *framework.Framework, ns string) {
	client := fw.AsKubeAdmin.CommonController.KubeRest()
	ctx := context.Background()

	if err := fw.AsKubeAdmin.HasController.DeleteAllApplicationsInASpecificNamespace(ns, cleanupResourceTimeout); err != nil {
		klog.Warningf("conformance cleanup: delete Applications in %s: %v", ns, err)
	}
	if err := fw.AsKubeAdmin.IntegrationController.DeleteAllSnapshotsInASpecificNamespace(ns, cleanupResourceTimeout); err != nil {
		klog.Warningf("conformance cleanup: delete Snapshots in %s: %v", ns, err)
	}
	if err := fw.AsKubeAdmin.TektonController.DeleteAllPipelineRunsInASpecificNamespace(ns); err != nil {
		klog.Warningf("conformance cleanup: delete PipelineRuns in %s: %v", ns, err)
	}
	if err := client.DeleteAllOf(ctx, &releaseApi.ReleasePlan{}, crclient.InNamespace(ns)); err != nil {
		klog.Warningf("conformance cleanup: delete ReleasePlans in %s: %v", ns, err)
	}
	if err := client.DeleteAllOf(ctx, &releaseApi.Release{}, crclient.InNamespace(ns)); err != nil {
		klog.Warningf("conformance cleanup: delete Releases in %s: %v", ns, err)
	}
}

// verifyReleasePrerequisites checks that the critical resources created by
// setup-release.sh actually exist. This catches silent failures in
// pre-provisioned mode where some script commands may fail non-fatally.
func verifyReleasePrerequisites(hub *framework.ControllerHub, managedNS string) error {
	ctx := context.Background()
	client := hub.CommonController.KubeRest()

	// Verify ECP exists in managed namespace.
	ecpList := &ecp.EnterpriseContractPolicyList{}
	if err := client.List(ctx, ecpList, crclient.InNamespace(managedNS)); err != nil {
		return fmt.Errorf("list ECPs in %s: %w", managedNS, err)
	}
	if len(ecpList.Items) == 0 {
		return fmt.Errorf("no EnterpriseContractPolicy found in %s; setup-release.sh may have failed", managedNS)
	}

	// Verify at least one ReleasePlanAdmission exists.
	rpaList := &releaseApi.ReleasePlanAdmissionList{}
	if err := client.List(ctx, rpaList, crclient.InNamespace(managedNS)); err != nil {
		return fmt.Errorf("list ReleasePlanAdmissions in %s: %w", managedNS, err)
	}
	if len(rpaList.Items) == 0 {
		return fmt.Errorf("no ReleasePlanAdmission found in %s; setup-release.sh may have failed", managedNS)
	}

	// Verify the managed namespace ServiceAccount exists.
	sa := &corev1.ServiceAccount{}
	saKey := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
	if err := client.Get(ctx, saKey, sa); err != nil {
		klog.Warningf("conformance: release-pipeline ServiceAccount not found in %s (may be externally managed): %v", managedNS, err)
	}

	return nil
}

// cleanupWithRetry runs fn until it returns nil, ctx is done, or a 30s per-step retry budget elapses.
// fn should use ctx for outbound calls so they honor the overall cleanup deadline.
func cleanupWithRetry(ctx context.Context, description string, fn func() error) {
	const maxStep = 30 * time.Second
	const poll = 5 * time.Second
	stepStart := time.Now()
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				klog.Warningf("conformance cleanup: %s stopped: %v (last attempt error: %v)", description, err, lastErr)
			} else {
				klog.Warningf("conformance cleanup: %s stopped: %v", description, err)
			}
			return
		}
		lastErr = fn()
		if lastErr == nil {
			return
		}
		stepElapsed := time.Since(stepStart)
		if stepElapsed >= maxStep {
			klog.Warningf("conformance cleanup: %s failed after retries: %v", description, lastErr)
			return
		}
		remainingStep := maxStep - stepElapsed
		d := poll
		if remainingStep < d {
			d = remainingStep
		}
		select {
		case <-ctx.Done():
			klog.Warningf("conformance cleanup: %s stopped: %v (last error: %v)", description, ctx.Err(), lastErr)
			return
		case <-time.After(d):
		}
	}
}
