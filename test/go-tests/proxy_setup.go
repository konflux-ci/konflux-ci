package go_tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/coder/websocket"
	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	konfluxCRName               = "konflux"
	defaultProxyTenantNamespace = "default-tenant"
	konfluxReadyWaitTimeout     = 5 * time.Minute
	konfluxHealthWaitTimeout    = 5 * time.Minute
	konfluxReadyPollInterval    = 2 * time.Second
	proxyHTTPRequestTimeout     = 30 * time.Second
	proxyAPITransientRetryTimeout = 2 * time.Minute
)

var (
	proxyBase       *url.URL
	proxyHome       string
	tokenURL        string
	proxyHTTPClient          *http.Client
	proxyWebSocketHTTPClient *http.Client
	proxyClient              crclient.Client
)

var _ = BeforeSuite(func() {
	var err error
	proxyClient, err = NewClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(proxyClient).NotTo(BeNil())

	wsTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	proxyHTTPClient = &http.Client{
		Timeout:   proxyHTTPRequestTimeout,
		Transport: wsTransport,
	}
	proxyWebSocketHTTPClient = &http.Client{Transport: wsTransport}

	ctx := context.Background()

	Eventually(func(g Gomega) {
		konflux := &konfluxv1alpha1.Konflux{}
		getErr := proxyClient.Get(ctx, crclient.ObjectKey{Name: konfluxCRName}, konflux)
		g.Expect(getErr).NotTo(HaveOccurred())

		if proxyWaitUIOnly() {
			g.Expect(konfluxUIReady(konflux)).To(BeTrue(), konfluxUIReadyMessage(konflux))
		} else {
			g.Expect(konflux.IsReady()).To(BeTrue(), konflux.ReadyConditionMessage())
		}

		baseURL := strings.TrimSpace(os.Getenv("KONFLUX_PROXY_URL"))
		if baseURL == "" {
			baseURL = konflux.Status.UIURL
			g.Expect(baseURL).NotTo(BeEmpty(), "expected konflux/konflux status.uiURL to be set")
		}
		g.Expect(setProxyBase(baseURL)).NotTo(HaveOccurred())
	}).WithTimeout(konfluxReadyWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())

	Eventually(func(g Gomega) {
		resp, doErr := proxyHTTPClient.Get(proxyURL("/health"))
		g.Expect(doErr).NotTo(HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}).WithTimeout(konfluxHealthWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())

	if isProxyOpenShiftAuth() {
		if _, ok := proxyIDTokenFromEnv(""); !ok {
			token, oauthErr := obtainOpenShiftProxyIDToken(ctx, proxyHTTPClient, proxyClient, proxyHome)
			Expect(oauthErr).NotTo(HaveOccurred())
			setProxyOpenShiftIDToken(token)
			logProxyAuthMethod(proxyAuthMethodOpenShiftOAuth)
		}
	}
})

func proxyWaitUIOnly() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("KONFLUX_PROXY_WAIT_UI_ONLY")))
	return v == "1" || v == "true" || v == "yes"
}

func konfluxUIReady(konflux *konfluxv1alpha1.Konflux) bool {
	cond := apimeta.FindStatusCondition(konflux.Status.Conditions, "ui.Ready")
	return cond != nil && cond.Status == metav1.ConditionTrue
}

func konfluxUIReadyMessage(konflux *konfluxv1alpha1.Konflux) string {
	cond := apimeta.FindStatusCondition(konflux.Status.Conditions, "ui.Ready")
	if cond == nil {
		return "konflux ui.Ready condition not found in status"
	}
	return fmt.Sprintf("konflux ui.Ready=%s reason=%s message=%s", cond.Status, cond.Reason, cond.Message)
}

func setProxyBase(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("proxy base URL must include scheme and host: %q", raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("proxy base URL must use https scheme: %q", raw)
	}
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	proxyBase = u
	proxyHome = proxyBase.String()
	tokenURL = proxyURL("/idp/token")
	return nil
}

func proxyTenantNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("KONFLUX_PROXY_TEST_NAMESPACE")); ns != "" {
		return ns
	}
	return defaultProxyTenantNamespace
}

// proxyCoreV1Path returns a namespaced core/v1 API path under the test tenant.
func proxyCoreV1Path(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/api/k8s/api/v1/namespaces/%s/%s", ns, resource)
}

// proxyUnauthenticatedNSPath returns the legacy unauthenticated proxy path shape.
func proxyUnauthenticatedNSPath(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/api/k8s/ns/%s/api/v1/namespaces/%s/%s", ns, ns, resource)
}

// proxyAppStudioPath returns a namespaced appstudio API path under the test tenant.
func proxyAppStudioPath(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/api/k8s/apis/appstudio.redhat.com/v1alpha1/namespaces/%s/%s", ns, resource)
}

// proxyUnauthenticatedAppStudioPath returns the legacy unauthenticated appstudio proxy path.
func proxyUnauthenticatedAppStudioPath(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/api/k8s/ns/%s/apis/appstudio.redhat.com/v1alpha1/namespaces/%s/%s", ns, ns, resource)
}

// proxyTektonPath returns a namespaced tekton.dev API path under the test tenant.
func proxyTektonPath(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/api/k8s/apis/tekton.dev/v1/namespaces/%s/%s", ns, resource)
}

// proxyTektonWSPath returns the WebSocket route for tekton.dev resources (/wss/k8s/* in Caddy).
func proxyTektonWSPath(resource string) string {
	ns := proxyTenantNamespace()
	return fmt.Sprintf("/wss/k8s/apis/tekton.dev/v1/namespaces/%s/%s", ns, resource)
}

// proxySecretsListExpectedStatus is the expected HTTP status for listing secrets in the
// test tenant. OpenShift CI uses kubeadmin (cluster-admin) → 200. Dex on default-tenant
// grants maintainer only → 403. Legacy user-ns* demo fixtures bind admin → 200.
func proxySecretsListExpectedStatus() int {
	if proxyAuthMethodFromEnv() == proxyAuthMethodOpenShiftOAuth {
		return http.StatusOK
	}
	if proxyTenantNamespace() == defaultProxyTenantNamespace {
		return http.StatusForbidden
	}
	return http.StatusOK
}

// expectProxyHTTPStatus asserts the response status without dumping bodies that may
// contain cluster secrets (Prow log censorship).
func expectProxyHTTPStatus(resp *http.Response, body []byte, expected int, path string) {
	GinkgoHelper()
	if resp.StatusCode == expected {
		return
	}
	Expect(resp.StatusCode).To(Equal(expected), proxyHTTPStatusMismatch(resp, body, expected, path))
}

func proxyHTTPStatusMismatch(resp *http.Response, body []byte, expected int, path string) string {
	if strings.Contains(path, "/secrets") {
		return fmt.Sprintf("%s: expected %d, got %d (%d bytes, body omitted)", path, expected, resp.StatusCode, len(body))
	}
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 240 {
		snippet = snippet[:240] + "..."
	}
	return fmt.Sprintf("%s: expected %d, got %d: %s", path, expected, resp.StatusCode, snippet)
}

func proxyTransientHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable
}

func proxyDrainResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

// expectProxyGETWithBearer GETs a proxied Kubernetes API path with retries on transient
// API server responses (e.g. 429 storage is (re)initializing right after CRD install).
func expectProxyGETWithBearer(path, token string, expected int) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(http.MethodGet, proxyURL(path), nil)
		g.Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := proxyHTTPClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())

		if proxyTransientHTTPStatus(resp.StatusCode) {
			g.Expect(resp.StatusCode).To(Equal(expected),
				fmt.Sprintf("%s: transient HTTP %d, retrying", path, resp.StatusCode))
			return
		}
		g.Expect(resp.StatusCode).To(Equal(expected), proxyHTTPStatusMismatch(resp, body, expected, path))
	}).WithTimeout(proxyAPITransientRetryTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
}

// expectProxyWebSocketDialWithBearer dials a proxied /wss/k8s/ endpoint with retries on
// transient API server responses during the WebSocket upgrade (e.g. HTTP 429).
func expectProxyWebSocketDialWithBearer(wsPath, token string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		ctx, cancel := context.WithTimeout(context.Background(), proxyHTTPRequestTimeout)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, proxyWebSocketURL(wsPath), &websocket.DialOptions{
			HTTPClient:   proxyWebSocketHTTPClient,
			Subprotocols: []string{"base64.binary.k8s.io"},
			HTTPHeader: http.Header{
				"Authorization": []string{"Bearer " + token},
				"Origin":        []string{proxyHome},
			},
		})

		if resp != nil && proxyTransientHTTPStatus(resp.StatusCode) {
			proxyDrainResponseBody(resp)
			g.Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols),
				fmt.Sprintf("%s: transient HTTP %d on WebSocket upgrade, retrying", wsPath, resp.StatusCode))
			return
		}
		if err != nil || conn == nil || resp == nil || resp.StatusCode != http.StatusSwitchingProtocols {
			proxyDrainResponseBody(resp)
		}
		g.Expect(err).NotTo(HaveOccurred(), "WebSocket connection should be established")
		if resp != nil {
			g.Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
		}
		g.Expect(conn).NotTo(BeNil())
		_ = conn.Close(websocket.StatusNormalClosure, "test complete")
	}).WithTimeout(proxyAPITransientRetryTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
}

func proxyURL(path string) string {
	if path == "" {
		return proxyBase.String()
	}
	return proxyBase.ResolveReference(mustParseProxyPath(path)).String()
}

func proxyWebSocketURL(path string) string {
	wsBase := *proxyBase
	wsBase.Scheme = "wss"
	return wsBase.ResolveReference(mustParseProxyPath(path)).String()
}

func mustParseProxyPath(path string) *url.URL {
	ref, err := url.Parse(path)
	if err != nil {
		panic(fmt.Sprintf("invalid proxy path %q: %v", path, err))
	}
	return ref
}
