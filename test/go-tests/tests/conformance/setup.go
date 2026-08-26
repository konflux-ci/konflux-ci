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
	appstudioApi "github.com/konflux-ci/application-api/api/v1alpha1"
	buildcontrollers "github.com/konflux-ci/build-service/controllers"
	imagecontroller "github.com/konflux-ci/image-controller/api/v1alpha1"
	integrationv1beta2 "github.com/konflux-ci/integration-service/api/v1beta2"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	pipelinev1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"

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

// pruneDanglingSASecrets removes references to non-existent Secrets from
// the release-pipeline ServiceAccount. This is always safe to call, even
// during concurrent runs, because it only removes references that point
// to Secrets that have already been deleted.
func pruneDanglingSASecrets(ctx context.Context, client crclient.Client, managedNS string) {
	sa := &corev1.ServiceAccount{}
	saKey := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
	if err := client.Get(ctx, saKey, sa); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance pre-run: get SA: %v", err)
		}
		return
	}
	pruned := make([]corev1.ObjectReference, 0, len(sa.Secrets))
	for _, ref := range sa.Secrets {
		secret := &corev1.Secret{}
		sKey := crclient.ObjectKey{Namespace: managedNS, Name: ref.Name}
		if err := client.Get(ctx, sKey, secret); err != nil {
			if k8sErrors.IsNotFound(err) {
				klog.Infof("conformance pre-run: pruning dangling secret ref %q from SA", ref.Name)
				continue
			}
			klog.Warningf("conformance pre-run: check secret %q existence: %v", ref.Name, err)
		}
		pruned = append(pruned, ref)
	}
	if len(pruned) != len(sa.Secrets) {
		sa.Secrets = pruned
		if err := client.Update(ctx, sa); err != nil {
			klog.Warningf("conformance pre-run: update SA after pruning: %v", err)
		}
	}
}

// setupReleaseResult holds the actual resource names created by runSetupRelease.
// Callers should use these instead of reconstructing names from suffix conventions.
type setupReleaseResult struct {
	ECPName            string
	TrustedArtifactsIR string
}

// runSetupRelease downloads setup-release.sh from the ConfigMap shipped by the
// operator (konflux-cli/setup-release) and executes it to create the managed
// namespace, ImageRepositories, EnterpriseContractPolicy, ReleasePlanAdmission,
// and ReleasePlan needed by the release flow. releaseName controls the name of the
// ReleasePlan and ReleasePlanAdmission resources (randomized per run to avoid collisions).
func runSetupRelease(appName, componentName, tenantNS, managedNS, releaseName string) (setupReleaseResult, error) {
	var result setupReleaseResult
	result.ECPName = "default"
	result.TrustedArtifactsIR = "trusted-artifacts"

	scriptContent, err := downloadScriptFromConfigMap("konflux-cli", "setup-release", "setup-release.sh")
	if err != nil {
		return result, fmt.Errorf("download setup-release.sh from ConfigMap: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "setup-release-*")
	if err != nil {
		return result, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	preProvisioned := isPreProvisioned()
	if preProvisioned {
		klog.Infof("conformance: managed namespace %s is pre-provisioned (E2E_MANAGED_NAMESPACE set), adapting setup-release.sh", managedNS)

		// Give per-run names to shared resources so concurrent test runs
		// don't clobber each other. Each run owns its own resources and
		// cleans them up independently.
		taSuffix := strings.TrimPrefix(releaseName, "release-")

		taName := "trusted-artifacts-" + taSuffix
		result.TrustedArtifactsIR = taName
		s := string(scriptContent)
		// Replace only the resource-defining contexts, not comments or log lines.
		s = strings.ReplaceAll(s, `name: trusted-artifacts`, `name: `+taName)
		s = strings.ReplaceAll(s, `/trusted-artifacts`, `/`+taName)
		s = strings.ReplaceAll(s, `("trusted-artifacts"`, `("`+taName+`"`)
		s = strings.ReplaceAll(s, `imagerepository trusted-artifacts`, `imagerepository `+taName)
		scriptContent = []byte(s)
		klog.Infof("conformance: renamed trusted-artifacts -> %s for run isolation", taName)

		ecpName := "ecp-" + taSuffix
		result.ECPName = ecpName
		oldJQ := `.metadata.namespace = "'"${MANAGED_NS}"'"'`
		newJQ := `.metadata.namespace = "'"${MANAGED_NS}"'" | .metadata.name = "` + ecpName + `"'`
		scriptContent = []byte(strings.Replace(string(scriptContent), oldJQ, newJQ, 1))
		// Point the ReleasePlanAdmission at the per-run ECP.
		scriptContent = []byte(strings.Replace(string(scriptContent), "policy: ${CONFORMA_POLICY}", "policy: "+ecpName, 1))
		klog.Infof("conformance: renamed ECP -> %s for run isolation", ecpName)

		type scriptPatch struct {
			name          string
			re            *regexp.Regexp
			repl          []byte
			expectedCount int // 0 means "any number > 0"
		}
		patches := []scriptPatch{
			{
				name:          "namespace-creation",
				re:            regexp.MustCompile(`(?s)kubectl apply -f - <<EOF\napiVersion: v1\nkind: Namespace\n.*?\nEOF`),
				repl:          []byte(`echo "Namespace ${MANAGED_NS} already exists, skipping creation"`),
				expectedCount: 1,
			},
			{
				// The || true must go on the kubectl line (after <<EOF), NOT after the
				// closing EOF marker -- placing it after EOF breaks the heredoc terminator.
				name:          "rolebinding-non-fatal",
				re:            regexp.MustCompile(`(?m)(kubectl apply -f - <<EOF\napiVersion: rbac\.authorization\.k8s\.io/v1\nkind: RoleBinding)`),
				repl:          []byte("kubectl apply -f - <<EOF || true\napiVersion: rbac.authorization.k8s.io/v1\nkind: RoleBinding"),
				expectedCount: 2,
			},
			{
				// SSO secret fetch may return Forbidden; the script already handles
				// empty SSO_TOKEN but set -e kills it on the kubectl error.
				name:          "sso-secret-non-fatal",
				re:            regexp.MustCompile("(\\{.data.\\$SSO_ACCOUNT\\}\")" + `\)`),
				repl:          []byte("${1} 2>/dev/null || true)"),
				expectedCount: 1,
			},
			{
				// Skip SA creation -- in pre-provisioned mode the SA is
				// ensured by ensureReleasePipelineSA in Go before the script
				// runs. The script's kubectl apply would overwrite .secrets
				// added by a concurrent run's ensureSASecret.
				name:          "skip-sa-creation",
				re:            regexp.MustCompile(`(?s)kubectl apply -f - <<EOF\napiVersion: v1\nkind: ServiceAccount\n.*?\nEOF`),
				repl:          []byte(`echo "ServiceAccount release-pipeline is pre-provisioned, skipping creation"`),
				expectedCount: 1,
			},
		}
		for _, p := range patches {
			matches := p.re.FindAllIndex(scriptContent, -1)
			if len(matches) == 0 {
				klog.Warningf("conformance: regex patch %q did not match setup-release.sh; upstream script format may have changed", p.name)
				continue
			}
			if p.expectedCount > 0 && len(matches) != p.expectedCount {
				klog.Warningf("conformance: regex patch %q matched %d times (expected %d); upstream script may have changed", p.name, len(matches), p.expectedCount)
			}
			scriptContent = p.re.ReplaceAll(scriptContent, p.repl)
		}
	}

	scriptPath := filepath.Join(tmpDir, "setup-release.sh")
	if err := os.WriteFile(scriptPath, scriptContent, 0o755); err != nil {
		return result, fmt.Errorf("write setup-release.sh: %w", err)
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
			return result, fmt.Errorf("setup-release.sh: %w", err)
		}
	}
	return result, nil
}

// ensureReleasePipelineSA creates the release-pipeline ServiceAccount in the
// managed namespace if it does not already exist. In pre-provisioned mode the
// script's kubectl-apply is skipped to avoid overwriting secrets from
// concurrent runs, so the SA must exist before the script runs.
func ensureReleasePipelineSA(ctx context.Context, client crclient.Client, managedNS string) error {
	sa := &corev1.ServiceAccount{}
	key := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
	if err := client.Get(ctx, key, sa); err == nil {
		klog.Infof("conformance: release-pipeline SA already exists in %s", managedNS)
		return nil
	} else if !k8sErrors.IsNotFound(err) {
		return fmt.Errorf("check release-pipeline SA in %s: %w", managedNS, err)
	}

	sa = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "release-pipeline",
			Namespace: managedNS,
		},
	}
	if err := client.Create(ctx, sa); err != nil {
		if k8sErrors.IsAlreadyExists(err) {
			return nil
		}
		return fmt.Errorf("create release-pipeline SA in %s: %w", managedNS, err)
	}
	klog.Infof("conformance: created release-pipeline SA in %s", managedNS)
	return nil
}

// ensureSASecret reads the push-secret names from this run's ImageRepositories
// and appends them to the release-pipeline ServiceAccount's secrets list.
// This is safe for concurrent test runs because it appends (never replaces) secrets,
// preventing one run from overwriting another run's credentials.
func ensureSASecret(ctx context.Context, client crclient.Client, managedNS string, irNames []string) error {
	secretNames := make([]string, 0, len(irNames))

	for _, irName := range irNames {
		ir := &imagecontroller.ImageRepository{}
		key := crclient.ObjectKey{Namespace: managedNS, Name: irName}
		if err := client.Get(ctx, key, ir); err != nil {
			klog.Warningf("conformance: get ImageRepository %s/%s: %v", managedNS, irName, err)
			continue
		}
		if name := ir.Status.Credentials.PushSecretName; name != "" {
			secretNames = append(secretNames, name)
		}
	}

	if len(secretNames) == 0 {
		return fmt.Errorf("no push-secret names found in ImageRepository status for %v", irNames)
	}

	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		sa := &corev1.ServiceAccount{}
		saKey := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
		if err := client.Get(ctx, saKey, sa); err != nil {
			return fmt.Errorf("get release-pipeline SA in %s: %w", managedNS, err)
		}

		// Prune dangling references -- secrets that no longer exist in the
		// namespace (left over from runs whose cleanup predated this fix).
		pruned := make([]corev1.ObjectReference, 0, len(sa.Secrets))
		for _, ref := range sa.Secrets {
			secret := &corev1.Secret{}
			sKey := crclient.ObjectKey{Namespace: managedNS, Name: ref.Name}
			if err := client.Get(ctx, sKey, secret); err != nil {
				if k8sErrors.IsNotFound(err) {
					klog.Infof("conformance: pruning dangling secret ref %q from release-pipeline SA", ref.Name)
					continue
				}
				klog.Warningf("conformance: check secret %q existence: %v", ref.Name, err)
			}
			pruned = append(pruned, ref)
		}
		sa.Secrets = pruned

		existing := make(map[string]bool, len(sa.Secrets))
		for _, ref := range sa.Secrets {
			existing[ref.Name] = true
		}

		for _, name := range secretNames {
			if !existing[name] {
				sa.Secrets = append(sa.Secrets, corev1.ObjectReference{Name: name})
				klog.Infof("conformance: appending secret %q to release-pipeline SA in %s", name, managedNS)
			}
		}

		if err := client.Update(ctx, sa); err != nil {
			if k8sErrors.IsConflict(err) && attempt < maxRetries-1 {
				klog.Warningf("conformance: SA update conflict (attempt %d/%d), retrying", attempt+1, maxRetries)
				continue
			}
			return fmt.Errorf("update release-pipeline SA in %s: %w", managedNS, err)
		}
		return nil
	}
	return nil
}

// removeSASecrets reads the push-secret names from this run's ImageRepositories
// and removes only those references from the release-pipeline SA, leaving
// secrets belonging to other concurrent runs intact.
func removeSASecrets(ctx context.Context, client crclient.Client, managedNS string, irNames []string) {
	toRemove := make(map[string]bool)

	for _, irName := range irNames {
		ir := &imagecontroller.ImageRepository{}
		key := crclient.ObjectKey{Namespace: managedNS, Name: irName}
		if err := client.Get(ctx, key, ir); err != nil {
			if !k8sErrors.IsNotFound(err) {
				klog.Warningf("conformance cleanup: get ImageRepository %s/%s for SA secret removal: %v", managedNS, irName, err)
			}
			continue
		}
		if name := ir.Status.Credentials.PushSecretName; name != "" {
			toRemove[name] = true
		}
	}

	if len(toRemove) == 0 {
		return
	}

	sa := &corev1.ServiceAccount{}
	saKey := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
	if err := client.Get(ctx, saKey, sa); err != nil {
		klog.Warningf("conformance cleanup: get release-pipeline SA for secret removal: %v", err)
		return
	}

	filtered := make([]corev1.ObjectReference, 0, len(sa.Secrets))
	for _, ref := range sa.Secrets {
		if !toRemove[ref.Name] {
			filtered = append(filtered, ref)
		} else {
			klog.Infof("conformance cleanup: removing secret %q from release-pipeline SA in %s", ref.Name, managedNS)
		}
	}

	if len(filtered) != len(sa.Secrets) {
		sa.Secrets = filtered
		if err := client.Update(ctx, sa); err != nil {
			if k8sErrors.IsConflict(err) {
				klog.Warningf("conformance cleanup: SA update conflict during secret removal, will be cleaned up by next run's pruneDanglingSASecrets")
			} else {
				klog.Warningf("conformance cleanup: update release-pipeline SA after secret removal: %v", err)
			}
		}
	}
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
	} else if !subjectsMatch(existingRB.Subjects, rb.Subjects) {
		// roleRef is immutable, but Subjects can be updated.
		existingRB.Subjects = rb.Subjects
		if err := client.Update(ctx, existingRB); err != nil {
			return fmt.Errorf("update RoleBinding %s/%s subjects: %w", namespace, rb.Name, err)
		}
	}

	return nil
}

// subjectsMatch returns true if two Subject slices have the same entries (order-sensitive).
func subjectsMatch(a, b []rbacv1.Subject) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Name != b[i].Name || a[i].Namespace != b[i].Namespace {
			return false
		}
	}
	return true
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
// appName scopes the cleanup to resources belonging to this test run's application,
// avoiding interference with concurrent test executions in the same namespace.
// componentName identifies this run's ImageRepositories so that only their push
// secrets are removed from the release-pipeline SA.
func cleanupManagedResources(ctx context.Context, fw *framework.Framework, ns, appName, componentName, releaseName string, releaseRes setupReleaseResult) {
	client := fw.AsKubeAdmin.CommonController.KubeRest()

	appLabel := crclient.MatchingLabels{"appstudio.openshift.io/application": appName}

	prList := &pipelinev1.PipelineRunList{}
	if err := client.List(ctx, prList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list PipelineRuns in %s: %v", ns, err)
	} else {
		for i := range prList.Items {
			pr := &prList.Items[i]
			completed := pr.Status.CompletionTime != nil
			if len(pr.Finalizers) > 0 && completed {
				pr.Finalizers = nil
				if err := client.Update(ctx, pr); err != nil {
					klog.Warningf("conformance cleanup: strip finalizers from PipelineRun %s/%s: %v", ns, pr.Name, err)
				}
			} else if len(pr.Finalizers) > 0 {
				klog.Warningf("conformance cleanup: PipelineRun %s/%s still running, skipping finalizer strip", ns, pr.Name)
			}
			if err := client.Delete(ctx, pr); err != nil {
				klog.Warningf("conformance cleanup: delete PipelineRun %s/%s: %v", ns, pr.Name, err)
			}
		}
	}
	irNames := []string{componentName, releaseRes.TrustedArtifactsIR}

	// Remove this run's push secrets from the SA before deleting ImageRepositories,
	// since we need to read the secret names from ImageRepository status.
	removeSASecrets(ctx, client, ns, irNames)
	// Delete only this run's ImageRepositories, not other runs'.
	for _, irName := range irNames {
		ir := &imagecontroller.ImageRepository{}
		irKey := crclient.ObjectKey{Namespace: ns, Name: irName}
		if err := client.Get(ctx, irKey, ir); err != nil {
			if !k8sErrors.IsNotFound(err) {
				klog.Warningf("conformance cleanup: get ImageRepository %s/%s: %v", ns, irName, err)
			}
		} else {
			if err := client.Delete(ctx, ir); err != nil {
				klog.Warningf("conformance cleanup: delete ImageRepository %s/%s: %v", ns, irName, err)
			}
		}
	}
	if err := fw.AsKubeAdmin.TektonController.DeleteEnterpriseContractPolicy(releaseRes.ECPName, ns, false); err != nil {
		klog.Warningf("conformance cleanup: delete ECP %s in %s: %v", releaseRes.ECPName, ns, err)
	}
	rpa := &releaseApi.ReleasePlanAdmission{}
	rpaKey := crclient.ObjectKey{Namespace: ns, Name: releaseName}
	if err := client.Get(ctx, rpaKey, rpa); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance cleanup: get ReleasePlanAdmission %s/%s: %v", ns, releaseName, err)
		}
	} else {
		if err := client.Delete(ctx, rpa); err != nil {
			klog.Warningf("conformance cleanup: delete ReleasePlanAdmission %s/%s: %v", ns, releaseName, err)
		}
	}
}

// cleanupTenantResources deletes test-created resources from the tenant namespace.
// appName scopes the cleanup to resources belonging to this test run's application,
// avoiding interference with concurrent test executions in the same namespace.
// componentName is used to delete the per-component ImageRepository.
// releaseName is used to delete the ReleasePlan by name as a fallback, since
// setup-release.sh does not add the appstudio.openshift.io/application label.
func cleanupTenantResources(ctx context.Context, fw *framework.Framework, ns, appName, componentName, releaseName string) {
	client := fw.AsKubeAdmin.CommonController.KubeRest()

	appLabel := crclient.MatchingLabels{"appstudio.openshift.io/application": appName}

	app := &appstudioApi.Application{}
	appKey := crclient.ObjectKey{Namespace: ns, Name: appName}
	if err := client.Get(ctx, appKey, app); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance cleanup: get Application %s/%s: %v", ns, appName, err)
		}
	} else {
		if err := client.Delete(ctx, app); err != nil {
			klog.Warningf("conformance cleanup: delete Application %s/%s: %v", ns, appName, err)
		}
	}
	itsList := &integrationv1beta2.IntegrationTestScenarioList{}
	if err := client.List(ctx, itsList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list IntegrationTestScenarios in %s: %v", ns, err)
	} else {
		for i := range itsList.Items {
			if err := client.Delete(ctx, &itsList.Items[i]); err != nil {
				klog.Warningf("conformance cleanup: delete IntegrationTestScenario %s/%s: %v", ns, itsList.Items[i].Name, err)
			}
		}
	}
	snapList := &appstudioApi.SnapshotList{}
	if err := client.List(ctx, snapList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list Snapshots in %s: %v", ns, err)
	} else {
		for i := range snapList.Items {
			if err := client.Delete(ctx, &snapList.Items[i]); err != nil {
				klog.Warningf("conformance cleanup: delete Snapshot %s/%s: %v", ns, snapList.Items[i].Name, err)
			}
		}
	}
	prList := &pipelinev1.PipelineRunList{}
	if err := client.List(ctx, prList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list PipelineRuns in %s: %v", ns, err)
	} else {
		for i := range prList.Items {
			if err := client.Delete(ctx, &prList.Items[i]); err != nil {
				klog.Warningf("conformance cleanup: delete PipelineRun %s/%s: %v", ns, prList.Items[i].Name, err)
			}
		}
	}
	rpList := &releaseApi.ReleasePlanList{}
	if err := client.List(ctx, rpList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list ReleasePlans in %s: %v", ns, err)
	} else {
		for i := range rpList.Items {
			if err := client.Delete(ctx, &rpList.Items[i]); err != nil {
				klog.Warningf("conformance cleanup: delete ReleasePlan %s/%s: %v", ns, rpList.Items[i].Name, err)
			}
		}
	}
	// Fallback: delete ReleasePlan by name since setup-release.sh does not add
	// the appstudio.openshift.io/application label.
	rp := &releaseApi.ReleasePlan{}
	rpKey := crclient.ObjectKey{Namespace: ns, Name: releaseName}
	if err := client.Get(ctx, rpKey, rp); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance cleanup: get ReleasePlan %s/%s: %v", ns, releaseName, err)
		}
	} else {
		if err := client.Delete(ctx, rp); err != nil {
			klog.Warningf("conformance cleanup: delete ReleasePlan %s/%s: %v", ns, releaseName, err)
		}
	}
	relList := &releaseApi.ReleaseList{}
	if err := client.List(ctx, relList, crclient.InNamespace(ns), appLabel); err != nil {
		klog.Warningf("conformance cleanup: list Releases in %s: %v", ns, err)
	} else {
		for i := range relList.Items {
			if err := client.Delete(ctx, &relList.Items[i]); err != nil {
				klog.Warningf("conformance cleanup: delete Release %s/%s: %v", ns, relList.Items[i].Name, err)
			}
		}
	}
	// Delete the Component (Application deletion does not cascade).
	comp := &appstudioApi.Component{}
	compKey := crclient.ObjectKey{Namespace: ns, Name: componentName}
	if err := client.Get(ctx, compKey, comp); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance cleanup: get Component %s/%s: %v", ns, componentName, err)
		}
	} else {
		if err := client.Delete(ctx, comp); err != nil {
			klog.Warningf("conformance cleanup: delete Component %s/%s: %v", ns, componentName, err)
		}
	}
	// Delete this run's ImageRepository from the tenant namespace (auto-created
	// by image-controller when the Component was created).
	ir := &imagecontroller.ImageRepository{}
	irKey := crclient.ObjectKey{Namespace: ns, Name: componentName}
	if err := client.Get(ctx, irKey, ir); err != nil {
		if !k8sErrors.IsNotFound(err) {
			klog.Warningf("conformance cleanup: get ImageRepository %s/%s: %v", ns, componentName, err)
		}
	} else {
		if err := client.Delete(ctx, ir); err != nil {
			klog.Warningf("conformance cleanup: delete ImageRepository %s/%s: %v", ns, componentName, err)
		}
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

	// Verify the release-pipeline ServiceAccount exists. Without it the release
	// PipelineRun will fail with a confusing "serviceaccount not found" error.
	sa := &corev1.ServiceAccount{}
	saKey := crclient.ObjectKey{Namespace: managedNS, Name: "release-pipeline"}
	if err := client.Get(ctx, saKey, sa); err != nil {
		return fmt.Errorf("release-pipeline ServiceAccount not found in %s: %w", managedNS, err)
	}

	return nil
}

// verifyPreProvisionedRBAC checks that the integration-runner Role and RoleBinding
// exist in the tenant namespace. In pre-provisioned environments, these are
// managed by GitOps and grantIntegrationRunnerJobRBAC may fail to create them.
func verifyPreProvisionedRBAC(hub *framework.ControllerHub, tenantNS string) error {
	ctx := context.Background()
	client := hub.CommonController.KubeRest()

	role := &rbacv1.Role{}
	if err := client.Get(ctx, crclient.ObjectKey{Namespace: tenantNS, Name: "integration-runner-jobs"}, role); err != nil {
		return fmt.Errorf("integration-runner-jobs Role not found in %s; integration tests will fail: %w", tenantNS, err)
	}

	rb := &rbacv1.RoleBinding{}
	if err := client.Get(ctx, crclient.ObjectKey{Namespace: tenantNS, Name: "integration-runner-jobs"}, rb); err != nil {
		return fmt.Errorf("integration-runner-jobs RoleBinding not found in %s; integration tests will fail: %w", tenantNS, err)
	}
	return nil
}

// verifyECPPatched checks that the ECP in the managed namespace has the expected
// E2E exclusions applied. Returns an error if any exclusion is missing, which
// gives a clear setup failure instead of an opaque downstream policy violation.
func verifyECPPatched(hub *framework.ControllerHub, policyName, managedNS string) error {
	policy, err := hub.TektonController.GetEnterpriseContractPolicy(policyName, managedNS)
	if err != nil {
		return fmt.Errorf("could not verify ECP patch in %s/%s: %w", managedNS, policyName, err)
	}
	verified := false
	for _, src := range policy.Spec.Sources {
		if src.Config == nil {
			continue
		}
		verified = true
		seen := make(map[string]bool, len(src.Config.Exclude))
		for _, e := range src.Config.Exclude {
			seen[e] = true
		}
		var missing []string
		for _, e := range e2eECPExclusions {
			if !seen[e] {
				missing = append(missing, e)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("ECP %s/%s is missing required exclusions %v; EC validation will fail", managedNS, policyName, missing)
		}
	}
	if !verified {
		return fmt.Errorf("ECP %s/%s has no sources with Config; patchECPForE2E may have failed", managedNS, policyName)
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
