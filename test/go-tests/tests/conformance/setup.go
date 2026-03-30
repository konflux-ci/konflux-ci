package conformance

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	ecp "github.com/conforma/crds/api/v1alpha1"
	buildcontrollers "github.com/konflux-ci/build-service/controllers"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kube"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/framework"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	releaseCatalogTAQuaySecret = "release-catalog-trusted-artifacts-quay-secret"
)

var (
	relSvcCatalogURL      = utils.GetEnv("RELEASE_SERVICE_CATALOG_URL", "https://github.com/konflux-ci/release-service-catalog")
	relSvcCatalogRevision = utils.GetReleaseServiceCatalogRevision()
)

func createReleaseConfig(hub *framework.ControllerHub, managedNamespace, userNamespace, componentName, appName string, secretData []byte, ociStorage string) {
	if ociStorage == "" {
		ociStorage = os.Getenv("RELEASE_TA_OCI_STORAGE")
	}
	if ociStorage != "" {
		ginkgo.GinkgoWriter.Printf("RELEASE_TA_OCI_STORAGE=%q\n", ociStorage)
	}

	klog.Info("conformance: creating release config", "managedNamespace", managedNamespace)

	_, err := hub.CommonController.CreateTestNamespace(managedNamespace)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to create managed namespace %s", managedNamespace)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "release-pull-secret", Namespace: managedNamespace},
		Data:       map[string][]byte{".dockerconfigjson": secretData},
		Type:       corev1.SecretTypeDockerConfigJson,
	}
	_, err = hub.CommonController.CreateSecret(managedNamespace, secret)
	gomega.Expect(err).ShouldNot(gomega.HaveOccurred(), "failed to create release-pull-secret")

	managedServiceAccount, err := hub.CommonController.CreateServiceAccount("release-service-account", managedNamespace, []corev1.ObjectReference{{Name: secret.Name}}, nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create release-service-account")

	_, err = hub.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(userNamespace, managedServiceAccount)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create role binding in %s", userNamespace)

	_, err = hub.ReleaseController.CreateReleasePipelineRoleBindingForServiceAccount(managedNamespace, managedServiceAccount)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create role binding in %s", managedNamespace)

	publicKey, err := hub.TektonController.GetTektonChainsPublicKey()
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), "failed to get Tekton Chains public key")

	err = hub.TektonController.CreateOrUpdateSigningSecret(publicKey, "cosign-public-key", managedNamespace)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create cosign-public-key secret")

	_, err = hub.ReleaseController.CreateReleasePlan("source-releaseplan", userNamespace, appName, managedNamespace, "", nil, nil, nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create ReleasePlan")

	defaultEcPolicy, err := hub.TektonController.GetEnterpriseContractPolicy("default", "enterprise-contract-service")
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get default EC policy")

	ecPolicyName := componentName + "-policy"
	sources := make([]ecp.Source, len(defaultEcPolicy.Spec.Sources))
	for i := range defaultEcPolicy.Spec.Sources {
		defaultEcPolicy.Spec.Sources[i].DeepCopyInto(&sources[i])
		if sources[i].Config == nil {
			sources[i].Config = &ecp.SourceConfig{}
		}
		// By default, `skip-checks` is set to true in the build pipeline which disables all the
		// tests/scans.
		sources[i].Config.Exclude = append(
			sources[i].Config.Exclude,
			"cve",
			"tasks.required_tasks_found:clair-scan",
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
		)
	}
	_, err = hub.TektonController.CreateEnterpriseContractPolicy(ecPolicyName, managedNamespace, ecp.EnterpriseContractPolicySpec{
		Description: "Red Hat's enterprise requirements",
		PublicKey:   string(publicKey),
		Sources:     sources,
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create EC policy %s", ecPolicyName)

	_, err = hub.ReleaseController.CreateReleasePlanAdmission("demo", managedNamespace, "", userNamespace, ecPolicyName, "release-service-account", []string{appName}, false, &tektonutils.PipelineRef{
		Resolver: "git",
		Params: []tektonutils.Param{
			{Name: "url", Value: relSvcCatalogURL},
			{Name: "revision", Value: relSvcCatalogRevision},
			{Name: "pathInRepo", Value: "pipelines/managed/e2e/e2e.yaml"},
		},
		OciStorage: ociStorage,
	}, nil)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create ReleasePlanAdmission")

	_, err = hub.TektonController.CreatePVCInAccessMode("release-pvc", managedNamespace, corev1.ReadWriteOnce)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create release-pvc")

	_, err = hub.CommonController.CreateRole("role-release-service-account", managedNamespace, map[string][]string{
		"apiGroupsList": {""},
		"roleResources": {"secrets"},
		"roleVerbs":     {"get", "list", "watch"},
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create Role")

	_, err = hub.CommonController.CreateRoleBinding("role-release-service-account-binding", managedNamespace, "ServiceAccount", "release-service-account", managedNamespace, "Role", "role-release-service-account", "rbac.authorization.k8s.io")
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), "failed to create RoleBinding")

	klog.Info("conformance: release config created", "managedNamespace", managedNamespace)
}

func createE2EQuaySecret(k *kube.CustomClient) (*corev1.Secret, error) {
	quayToken := os.Getenv("QUAY_TOKEN")
	if quayToken == "" {
		return nil, fmt.Errorf("QUAY_TOKEN env is not set")
	}

	decodedToken, err := base64.StdEncoding.DecodeString(quayToken)
	if err != nil {
		return nil, fmt.Errorf("failed to decode QUAY_TOKEN (must be base64): %v", err)
	}

	namespace := constants.QuayRepositorySecretNamespace
	_, err = k.KubeInterface().CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			_, err = k.KubeInterface().CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: namespace},
			}, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("error creating namespace %s: %v", namespace, err)
			}
		} else {
			return nil, fmt.Errorf("error getting namespace %s: %v", namespace, err)
		}
	}

	secretName := constants.QuayRepositorySecretName
	secret, err := k.KubeInterface().CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	if err != nil {
		if k8sErrors.IsNotFound(err) {
			secret, err = k.KubeInterface().CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
				Type:       corev1.SecretTypeDockerConfigJson,
				Data:       map[string][]byte{corev1.DockerConfigJsonKey: decodedToken},
			}, metav1.CreateOptions{})
			if err != nil {
				return nil, fmt.Errorf("error creating secret %s: %v", secretName, err)
			}
		} else {
			secret.Data = map[string][]byte{corev1.DockerConfigJsonKey: decodedToken}
			secret, err = k.KubeInterface().CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
			if err != nil {
				return nil, fmt.Errorf("error updating secret %s: %v", secretName, err)
			}
		}
	}

	return secret, nil
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

func cleanupWithRetry(description string, fn func() error) {
	err := gomega.InterceptGomegaFailure(func() {
		gomega.Eventually(fn).
			WithTimeout(30 * time.Second).
			WithPolling(5 * time.Second).
			Should(gomega.Succeed())
	})
	if err != nil {
		klog.Warningf("conformance cleanup: %s failed after retries: %v", description, err)
	}
}
