package go_tests

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

func TestAdditionalFQDNFromPrimaryHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		primary string
		want    string
		wantErr bool
	}{
		{
			name:    "apps domain host",
			primary: "konflux-ui-konflux-ui.apps.ci-ln-example.example.openshiftapps.com",
			want:    "konflux-ui-additional.apps.ci-ln-example.example.openshiftapps.com",
		},
		{
			name:    "strips port",
			primary: "konflux-ui.example.com:443",
			want:    "konflux-ui-additional.example.com",
		},
		{
			name:    "empty",
			primary: "",
			wantErr: true,
		},
		{
			name:    "single label",
			primary: "localhost",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := additionalFQDNFromPrimaryHost(tt.primary)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildAdditionalFQDNIngress(t *testing.T) {
	t.Parallel()

	ing := buildAdditionalFQDNIngress("konflux-ui", "konflux-ui-additional.apps.example.com")
	if ing.Name != additionalFQDNIngressName {
		t.Fatalf("name = %q", ing.Name)
	}
	if ing.Namespace != "konflux-ui" {
		t.Fatalf("namespace = %q", ing.Namespace)
	}
	if got := ing.Spec.Rules[0].Host; got != "konflux-ui-additional.apps.example.com" {
		t.Fatalf("host = %q", got)
	}
	if got := ing.Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name; got != ingress.ProxyServiceName {
		t.Fatalf("service = %q", got)
	}
	if ing.Annotations["route.openshift.io/termination"] != "reencrypt" {
		t.Fatalf("missing reencrypt annotation: %#v", ing.Annotations)
	}
}

func TestProxyAdditionalFQDNTestsEnabled(t *testing.T) {
	t.Setenv("REPO_NAME", "")
	t.Setenv("KONFLUX_PROXY_TEST_ADDITIONAL_FQDN", "")
	if proxyAdditionalFQDNTestsEnabled() {
		t.Fatal("expected disabled by default")
	}

	t.Setenv("REPO_NAME", "other-repo")
	if proxyAdditionalFQDNTestsEnabled() {
		t.Fatal("expected disabled for non-konflux-ci REPO_NAME")
	}

	t.Setenv("REPO_NAME", "konflux-ci")
	if !proxyAdditionalFQDNTestsEnabled() {
		t.Fatal("expected enabled when REPO_NAME=konflux-ci")
	}

	t.Setenv("REPO_NAME", "other-repo")
	t.Setenv("KONFLUX_PROXY_TEST_ADDITIONAL_FQDN", "true")
	if !proxyAdditionalFQDNTestsEnabled() {
		t.Fatal("expected enabled when KONFLUX_PROXY_TEST_ADDITIONAL_FQDN=true")
	}

	t.Setenv("KONFLUX_PROXY_TEST_ADDITIONAL_FQDN", " True ")
	if !proxyAdditionalFQDNTestsEnabled() {
		t.Fatal("expected enabled when KONFLUX_PROXY_TEST_ADDITIONAL_FQDN is case-insensitive true")
	}
}

// proxy-additional-fqdn marks CR-mutating specs. The runner excludes this label
// unless REPO_NAME=konflux-ci or KONFLUX_PROXY_TEST_ADDITIONAL_FQDN=true so
// environments with an externally managed Konflux CR do not run them.
var _ = Describe("Proxy additional FQDNs", Label("proxy-additional-fqdn"), Ordered, func() {
	var (
		uiNamespace string
		aliasHost   string
		aliasBase   string
		aliasToken  string
	)

	BeforeAll(func() {
		if !isProxyOpenShiftAuth() {
			Skip("requires KONFLUX_PROXY_AUTH=openshift")
		}
		if !proxyAdditionalFQDNTestsEnabled() {
			Skip("additional-FQDN specs run only for konflux-ci OpenShift CI (REPO_NAME=konflux-ci) or when KONFLUX_PROXY_TEST_ADDITIONAL_FQDN=true")
		}
		Expect(proxyBase).NotTo(BeNil(), "proxy base URL must be set by BeforeSuite")
		Expect(proxyHome).NotTo(BeEmpty())

		var err error
		aliasHost, err = additionalFQDNFromPrimaryHost(proxyBase.Host)
		Expect(err).NotTo(HaveOccurred())
		Expect(aliasHost).NotTo(Equal(proxyBase.Host))
		aliasBase = additionalFQDNURL(aliasHost)
		uiNamespace = uiNamespaceForProxyTests()
	})

	It("keeps the primary UI functional", func(ctx context.Context) {
		Eventually(func(g Gomega) {
			resp, err := proxyHTTPClient.Get(proxyURL("/health"))
			g.Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
		}).WithTimeout(konfluxHealthWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())

		token, err := ExtractToken(proxyClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL("/api/k8s/api/v1/namespaces"), nil)
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := proxyHTTPClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
	})

	Context("with an additional FQDN and user-managed Ingress", func() {
		BeforeAll(func(ctx context.Context) {
			By("recording original Konflux UI ingress config for restore")
			konflux := &konfluxv1alpha1.Konflux{}
			Expect(proxyClient.Get(ctx, client.ObjectKey{Name: konfluxCRName}, konflux)).To(Succeed())
			original := konflux.DeepCopy()
			var originalAdditional []string
			if original.Spec.KonfluxUI != nil &&
				original.Spec.KonfluxUI.Spec != nil &&
				original.Spec.KonfluxUI.Spec.Ingress != nil {
				originalAdditional = append([]string(nil), original.Spec.KonfluxUI.Spec.Ingress.AdditionalFQDNs...)
			}

			By("setting additionalFQDNs on the Konflux CR")
			ensureUIIngressSpec(konflux)
			konflux.Spec.KonfluxUI.Spec.Ingress.AdditionalFQDNs = []string{aliasHost}
			Expect(proxyClient.Patch(ctx, konflux, client.MergeFrom(original))).To(Succeed())

			DeferCleanup(func(ctx context.Context) {
				By("restoring Konflux additionalFQDNs")
				current := &konfluxv1alpha1.Konflux{}
				Expect(proxyClient.Get(ctx, client.ObjectKey{Name: konfluxCRName}, current)).To(Succeed())
				base := current.DeepCopy()
				ensureUIIngressSpec(current)
				// Use an empty slice (not nil) so JSON merge patch clears prior values.
				current.Spec.KonfluxUI.Spec.Ingress.AdditionalFQDNs = append([]string(nil), originalAdditional...)
				if current.Spec.KonfluxUI.Spec.Ingress.AdditionalFQDNs == nil {
					current.Spec.KonfluxUI.Spec.Ingress.AdditionalFQDNs = []string{}
				}
				Expect(proxyClient.Patch(ctx, current, client.MergeFrom(base))).To(Succeed())
			})

			By("creating a user-managed Ingress for the additional FQDN")
			ing := buildAdditionalFQDNIngress(uiNamespace, aliasHost)
			Expect(proxyClient.Create(ctx, ing)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				err := proxyClient.Delete(ctx, &networkingv1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      additionalFQDNIngressName,
						Namespace: uiNamespace,
					},
				})
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
			})

			By("waiting for operator auth config, Dex/proxy rollout, and alias Ingress to become usable")
			Eventually(func(g Gomega) {
				assertAdditionalFQDNOperatorConfig(g, ctx, aliasHost)
				resp, err := proxyHTTPClient.Get(aliasBase + "/health")
				g.Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}).WithTimeout(5 * time.Minute).WithPolling(konfluxReadyPollInterval).Should(Succeed())
		})

		It("completes OpenShift OAuth with the additional FQDN callback", func(ctx context.Context) {
			// Dex issuer stays on the primary host; the alias is only valid as
			// oauth2-proxy's redirect_uri (same as a browser login via oauth2-proxy).
			token, err := obtainOpenShiftProxyIDTokenForUI(ctx, proxyHTTPClient, proxyClient, proxyHome, aliasBase)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())
			aliasToken = token
		})

		It("proxies authenticated API requests on the additional FQDN", func(ctx context.Context) {
			Expect(aliasToken).NotTo(BeEmpty(), "expected OAuth token from the previous spec")

			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet, aliasBase+"/api/k8s/api/v1/namespaces", nil,
			)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+aliasToken)
			resp, err := proxyHTTPClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			Expect(resp.StatusCode).To(Equal(http.StatusOK), "body: %s", truncateForErr(body))
		})

		It("keeps the primary UI functional while additionalFQDNs are configured", func(ctx context.Context) {
			Eventually(func(g Gomega) {
				resp, err := proxyHTTPClient.Get(proxyURL("/health"))
				g.Expect(err).NotTo(HaveOccurred())
				defer resp.Body.Close()
				g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}).WithTimeout(konfluxHealthWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())

			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := proxyHTTPClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})

func ensureUIIngressSpec(konflux *konfluxv1alpha1.Konflux) {
	if konflux.Spec.KonfluxUI == nil {
		konflux.Spec.KonfluxUI = &konfluxv1alpha1.KonfluxUIConfig{}
	}
	if konflux.Spec.KonfluxUI.Spec == nil {
		konflux.Spec.KonfluxUI.Spec = &konfluxv1alpha1.KonfluxUIConfigSpec{}
	}
	if konflux.Spec.KonfluxUI.Spec.Ingress == nil {
		konflux.Spec.KonfluxUI.Spec.Ingress = &konfluxv1alpha1.IngressSpec{}
	}
}

func assertAdditionalFQDNOperatorConfig(g Gomega, ctx context.Context, aliasHost string) {
	ns := uiNamespaceForProxyTests()
	callback := fmt.Sprintf("https://%s/oauth2/callback", aliasHost)

	cmList := &corev1.ConfigMapList{}
	g.Expect(proxyClient.List(ctx, cmList,
		client.InNamespace(ns),
		client.MatchingLabels{uiDexConfigMapLabel: "true"},
	)).To(Succeed())
	g.Expect(cmList.Items).NotTo(BeEmpty(), "expected Dex ConfigMap")

	var dexConfigMapName string
	for i := range cmList.Items {
		if data := cmList.Items[i].Data[uiDexConfigMapKey]; strings.Contains(data, callback) {
			dexConfigMapName = cmList.Items[i].Name
			break
		}
	}
	g.Expect(dexConfigMapName).NotTo(BeEmpty(), "expected Dex ConfigMap containing alias callback %s", callback)

	dexDep := &appsv1.Deployment{}
	g.Expect(proxyClient.Get(ctx, client.ObjectKey{
		Name:      "dex",
		Namespace: ns,
	}, dexDep)).To(Succeed())
	dexVol := kubernetes.FindVolume(dexDep.Spec.Template.Spec.Volumes, "dex")
	g.Expect(dexVol).NotTo(BeNil(), "expected dex ConfigMap volume")
	g.Expect(dexVol.ConfigMap).NotTo(BeNil())
	g.Expect(dexVol.ConfigMap.Name).To(Equal(dexConfigMapName),
		"dex Deployment must mount the ConfigMap that includes the alias callback")
	assertDeploymentRolledOut(g, dexDep)

	proxyDep := &appsv1.Deployment{}
	g.Expect(proxyClient.Get(ctx, client.ObjectKey{
		Name:      "proxy",
		Namespace: ns,
	}, proxyDep)).To(Succeed())
	oauth2 := kubernetes.FindContainer(proxyDep.Spec.Template.Spec.Containers, "oauth2-proxy")
	g.Expect(oauth2).NotTo(BeNil())

	env := map[string]string{}
	for _, e := range oauth2.Env {
		env[e.Name] = e.Value
	}
	g.Expect(env["OAUTH2_PROXY_REDIRECT_URL"]).To(Equal("/oauth2/callback"))
	g.Expect(env["OAUTH2_PROXY_REVERSE_PROXY"]).To(Equal("true"))
	g.Expect(env["OAUTH2_PROXY_WHITELIST_DOMAINS"]).To(ContainSubstring(aliasHost))
	assertDeploymentRolledOut(g, proxyDep)
}

// assertDeploymentRolledOut requires the Deployment controller to have observed the
// current generation and for all replicas to be updated and available.
func assertDeploymentRolledOut(g Gomega, dep *appsv1.Deployment) {
	replicas := int32(1)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}
	g.Expect(dep.Status.ObservedGeneration).To(BeNumerically(">=", dep.Generation),
		"%s/%s observedGeneration", dep.Namespace, dep.Name)
	g.Expect(dep.Status.UpdatedReplicas).To(Equal(replicas),
		"%s/%s updatedReplicas", dep.Namespace, dep.Name)
	g.Expect(dep.Status.AvailableReplicas).To(Equal(replicas),
		"%s/%s availableReplicas", dep.Namespace, dep.Name)
	g.Expect(dep.Status.UnavailableReplicas).To(BeZero(),
		"%s/%s unavailableReplicas", dep.Namespace, dep.Name)
}

func truncateForErr(body []byte) string {
	const max = 512
	s := string(body)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
