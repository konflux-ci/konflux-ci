package go_tests

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authv1 "k8s.io/api/authorization/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	userName = "user2@konflux.dev"
	password = "password"
)

// expectedImpersonateUser returns the username the proxy sets in the
// Impersonate-User header. On OpenShift the OAuth flow authenticates as
// kubeadmin which maps to the "kube:admin" identity; Dex uses the email.
func expectedImpersonateUser() string {
	if isProxyOpenShiftAuth() {
		return "kube:admin"
	}
	return userName
}

var _ = Describe("Test Proxy endpoints", func() {
	DescribeTable("Test endpoints without token",
		func(path string, expectedStatus int) {
			request, err := http.NewRequest("GET", proxyURL(path), nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(expectedStatus))
		},
		Entry("Home", "", 200),
		Entry("Health", "/health", 200),
		Entry("Secrets", proxyUnauthenticatedNSPath("secrets"), 401),
		Entry("Release", proxyUnauthenticatedAppStudioPath("releaseplans"), 401),
		Entry("Applications", proxyUnauthenticatedAppStudioPath("applications"), 401),
		Entry("Namespaces", "/api/k8s/api/v1/namespaces", 401),
	)

	DescribeTable("Test endpoints with token",
		func(path string, expectedStatus int) {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).ToNot(BeEmpty())

			expectProxyGETWithBearer(path, token, expectedStatus)
		},
		Entry("Home", "", 200),
		Entry("Health", "/health", 200),
		Entry("Secrets", proxyCoreV1Path("secrets"), proxySecretsListExpectedStatus()),
		Entry("Release", proxyAppStudioPath("releaseplans"), 200),
		Entry("Applications", proxyAppStudioPath("applications"), 200),
		Entry("Namespaces", "/api/k8s/api/v1/namespaces", 200),
		Entry("PipelineRuns", proxyTektonPath("pipelineruns"), 200),
	)

	Describe("Test WebSocket endpoint", func() {
		It("should establish a WebSocket connection through /wss/k8s/", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).ToNot(BeEmpty())

			wsPath := proxyTektonWSPath("pipelineruns") + "?watch=true"
			expectProxyWebSocketDialWithBearer(wsPath, token)
		})

		It("should reject WebSocket connection without token", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			wsPath := proxyTektonWSPath("pipelineruns") + "?watch=true"

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}

			_, resp, err := websocket.Dial(ctx, proxyWebSocketURL(wsPath), &websocket.DialOptions{
				HTTPClient: httpClient,
				HTTPHeader: http.Header{
					"Origin": []string{proxyHome},
				},
			})
			Expect(err).To(HaveOccurred(), "WebSocket connection should be rejected without token")
			if resp != nil {
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			}
		})
	})

	Describe("Test Impersonate header stripping", func() {
		It("should reject client-sent Impersonate-User headers", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Impersonate-User", "system:admin")

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(200),
				"Impersonate-User from client should be stripped; request should succeed as the authenticated user, not as system:admin")
		})

		It("should reject client-sent Impersonate-Group headers", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Impersonate-Group", "system:masters")

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(200),
				"Impersonate-Group from client should be stripped; request should succeed normally")
		})
	})

	Describe("Test proxy ClusterRole group impersonation ceiling", func() {
		const proxySAUser = "system:serviceaccount:konflux-ui:proxy"

		It("should allow impersonating users and only system:authenticated groups", func(ctx context.Context) {
			expectImpersonateSAR(ctx, proxySAUser, "groups", "system:masters", false)
			expectImpersonateSAR(ctx, proxySAUser, "groups", "team-alpha", false)
			expectImpersonateSAR(ctx, proxySAUser, "groups", "system:authenticated", true)
			expectImpersonateSAR(ctx, proxySAUser, "users", "user1@konflux.dev", true)
		})
	})

	Describe("Test group impersonation ceiling", Label("proxy-dex"), func() {
		const idpGroupUser = "user1@konflux.dev"
		const idpGroupName = "team-alpha"

		It("should succeed for a user who has IdP groups without forwarding them", func() {
			token, err := ExtractTokenForUser(proxyClient, idpGroupUser, "password")
			Expect(err).NotTo(HaveOccurred())

			// Hit the kube API (not namespace-lister). A 403 here means the
			// apiserver rejected Impersonate-Group values the proxy SA is
			// not allowed to impersonate (e.g. IdP groups still forwarded).
			appsPath := proxyAppStudioPath("applications")
			request, err := http.NewRequest("GET", proxyURL(appsPath), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			expectProxyHTTPStatus(response, body, http.StatusOK, appsPath)
		})

		It("should ignore IdP group RoleBindings and honor system:authenticated", func(ctx context.Context) {
			token, err := ExtractTokenForUser(proxyClient, idpGroupUser, "password")
			Expect(err).NotTo(HaveOccurred())

			nsName := fmt.Sprintf("proxy-group-ceiling-%d", GinkgoRandomSeed())
			// namespace-lister only indexes namespaces matching
			// CACHE_NAMESPACE_LABELSELECTOR (konflux-ci.dev/type=tenant).
			ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name: nsName,
				Labels: map[string]string{
					"konflux-ci.dev/type": "tenant",
				},
			}}
			Expect(proxyClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				err := proxyClient.Delete(ctx, ns)
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
			})

			// Narrow Role: configmaps for the kube API assertion, plus
			// namespaces get so namespace-lister can surface the ns when the
			// subject is bound (lister requires get on namespaces).
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "proxy-group-ceiling-read",
					Namespace: nsName,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups: []string{""},
						Resources: []string{"configmaps"},
						Verbs:     []string{"get", "list"},
					},
					{
						APIGroups: []string{""},
						Resources: []string{"namespaces"},
						Verbs:     []string{"get"},
					},
				},
			}
			Expect(proxyClient.Create(ctx, role)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				err := proxyClient.Delete(ctx, role)
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
			})

			authRB := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "system-authenticated-read",
					Namespace: nsName,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     role.Name,
				},
				Subjects: []rbacv1.Subject{{
					APIGroup: rbacv1.GroupName,
					Kind:     rbacv1.GroupKind,
					Name:     "system:authenticated",
				}},
			}
			Expect(proxyClient.Create(ctx, authRB)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				err := proxyClient.Delete(ctx, authRB)
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
			})

			configMapsPath := fmt.Sprintf("/api/k8s/api/v1/namespaces/%s/configmaps", nsName)

			By("proving the namespace is indexed and system:authenticated grants access")
			Eventually(func(g Gomega) {
				code, body := proxyGETStatusAndBodyG(g, configMapsPath, token)
				g.Expect(code).To(Equal(http.StatusOK), string(body))
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			Eventually(func(g Gomega) {
				code, body := proxyGETStatusAndBodyG(g, "/api/k8s/api/v1/namespaces", token)
				g.Expect(code).To(Equal(http.StatusOK), string(body))
				g.Expect(namespaceListContains(g, body, nsName)).To(BeTrue(),
					"namespace-lister should honor system:authenticated RoleBindings")
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

			By("removing the system:authenticated binding")
			Expect(proxyClient.Delete(ctx, authRB)).To(Succeed())
			Eventually(func(g Gomega) {
				err := proxyClient.Get(ctx, crclient.ObjectKeyFromObject(authRB), &rbacv1.RoleBinding{})
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "unexpected error: %v", err)
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

			By("confirming access and listing drop after the binding is gone")
			Eventually(func(g Gomega) {
				code, body := proxyGETStatusAndBodyG(g, configMapsPath, token)
				g.Expect(code).To(Equal(http.StatusForbidden), string(body))
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
			Eventually(func(g Gomega) {
				code, body := proxyGETStatusAndBodyG(g, "/api/k8s/api/v1/namespaces", token)
				g.Expect(code).To(Equal(http.StatusOK), string(body))
				g.Expect(namespaceListContains(g, body, nsName)).To(BeFalse())
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

			groupRB := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "idp-group-read",
					Namespace: nsName,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: rbacv1.GroupName,
					Kind:     "Role",
					Name:     role.Name,
				},
				Subjects: []rbacv1.Subject{{
					APIGroup: rbacv1.GroupName,
					Kind:     rbacv1.GroupKind,
					Name:     idpGroupName,
				}},
			}
			Expect(proxyClient.Create(ctx, groupRB)).To(Succeed())
			DeferCleanup(func(ctx context.Context) {
				err := proxyClient.Delete(ctx, groupRB)
				Expect(crclient.IgnoreNotFound(err)).NotTo(HaveOccurred())
			})

			By("proving the IdP group RoleBinding is live via SubjectAccessReview")
			Eventually(func(g Gomega) {
				sar := &authv1.SubjectAccessReview{
					Spec: authv1.SubjectAccessReviewSpec{
						User:   idpGroupUser,
						Groups: []string{idpGroupName, "system:authenticated"},
						ResourceAttributes: &authv1.ResourceAttributes{
							Namespace: nsName,
							Verb:      "list",
							Group:     "",
							Resource:  "configmaps",
						},
					},
				}
				g.Expect(proxyClient.Create(ctx, sar)).To(Succeed())
				g.Expect(sar.Status.Allowed).To(BeTrue(),
					"SAR with IdP group should be allowed once the RoleBinding exists: %s", sar.Status.Reason)
			}).WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Succeed())

			By("keeping IdP group grants ineffective through the proxy")
			Consistently(func(g Gomega) {
				code, body := proxyGETStatusAndBodyG(g, configMapsPath, token)
				g.Expect(code).To(Equal(http.StatusForbidden),
					"IdP group RoleBinding must not grant access through the proxy: %s", string(body))

				listCode, listBody := proxyGETStatusAndBodyG(g, "/api/k8s/api/v1/namespaces", token)
				g.Expect(listCode).To(Equal(http.StatusOK), string(listBody))
				g.Expect(namespaceListContains(g, listBody, nsName)).To(BeFalse(),
					"namespace-lister must not treat IdP groups as authorization subjects")
			}).WithTimeout(10 * time.Second).WithPolling(2 * time.Second).Should(Succeed())
		})
	})

	Describe("Test user with no IdP groups", Label("proxy-dex"), func() {
		It("should succeed when the user has no IdP groups", func() {
			token, err := ExtractTokenForUser(proxyClient, userName, password)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL(proxyAppStudioPath("applications")), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			expectProxyHTTPStatus(response, body, http.StatusOK, proxyAppStudioPath("applications"))
		})
	})

	Describe("Test public CLI client", Label("proxy-dex"), func() {
		It("should accept a Dex ID token issued to the public cli client", func() {
			// Password grant is test-only (Kind static users). Production CLIs use
			// PKCE or device code; this asserts the same JWT audience and Bearer path.
			token, err := GetIdTokenForPublicClient(dex.CLIClientID, userName, password)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())
			hasCLIAud, audErr := jwtAudienceContains(token, dex.CLIClientID)
			Expect(audErr).NotTo(HaveOccurred())
			Expect(hasCLIAud).To(BeTrue(),
				"id_token aud must be the public CLI client, not oauth2-proxy")

			expectProxyGETWithBearer("/api/k8s/api/v1/namespaces", token, http.StatusOK)
		})
	})

	Describe("Test namespace-lister endpoint", func() {
		It("should return namespaces for authenticated user", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			expectProxyHTTPStatus(response, body, http.StatusOK, "/api/k8s/api/v1/namespaces")

			var nsList map[string]interface{}
			err = json.Unmarshal(body, &nsList)
			Expect(err).NotTo(HaveOccurred())
			Expect(nsList).To(HaveKey("items"), "response should be a namespace list")
		})

		It("should route non-GET methods to Kube API, not namespace-lister", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("POST", proxyURL("/api/k8s/api/v1/namespaces"),
				strings.NewReader(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"test-reject"}}`))
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).NotTo(Equal(405),
				"POST should be routed to the Kube API, not namespace-lister")
		})

		It("should require authentication for namespace-lister", func() {
			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(401))
		})
	})

	Describe("Test Tekton Results endpoint", func() {
		const resultsPath = "/api/k8s/plugins/tekton-results/apis/results.tekton.dev/v1alpha2/parents/-/results"

		It("should proxy authenticated requests to Tekton Results", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL(resultsPath), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			Expect(response.StatusCode).To(SatisfyAny(
				Equal(200), Equal(502), Equal(401)),
				"Tekton Results endpoint should be proxied")
		})

		It("should reject unauthenticated requests to Tekton Results", func() {
			request, err := http.NewRequest("GET", proxyURL(resultsPath), nil)
			Expect(err).NotTo(HaveOccurred())

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(401))
		})
	})

	Describe("Test metrics endpoint", func() {
		It("should expose Prometheus metrics on the metrics port", func() {
			raw, err := serviceProxyGet(context.TODO(), "konflux-ui", "proxy", "metrics", "/metrics")
			Expect(err).NotTo(HaveOccurred())
			Expect(string(raw)).To(ContainSubstring("caddy_"),
				"metrics should contain Caddy-specific metrics")
		})
	})

	Describe("Test Kite endpoint", func() {
		kitePath := kiteEndpoint.BasePath

		BeforeEach(func() {
			if epModes.Kite == modeSkip {
				Skip("Kite endpoint not enabled in Konflux CR")
			}
		})

		It("should proxy authenticated requests with impersonation headers", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			if epModes.Kite == modeEcho {
				// Poll to tolerate late service-serving cert projection on
				// OpenShift, matching the Watson test pattern.
				var lastHeaders map[string][]string
				Eventually(func(g Gomega) {
					lastHeaders = echoGetG(g, kitePath+"echo", token)
					g.Expect(lastHeaders).To(HaveKey("Authorization"),
						"echo should receive Authorization header with kube_token")
					g.Expect(lastHeaders["Authorization"]).To(HaveLen(1))
					g.Expect(lastHeaders["Authorization"][0]).To(HavePrefix("Bearer "),
						"Authorization should be a Bearer token (kube SA token)")
					g.Expect(lastHeaders).To(HaveKey("Impersonate-User"),
						"echo should receive Impersonate-User header")
					g.Expect(lastHeaders["Impersonate-User"][0]).To(Equal(expectedImpersonateUser()),
						"Impersonate-User should match the authenticated user")
					g.Expect(lastHeaders).To(HaveKey("Impersonate-Group"),
						"echo should receive Impersonate-Group header")
					g.Expect(lastHeaders["Impersonate-Group"]).To(Equal([]string{"system:authenticated"}),
						"Impersonate-Group must be only system:authenticated")
				}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			} else {
				expectEndpointRouted(kitePath, token)
			}
		})

		It("should reject unauthenticated requests", func() {
			request, err := http.NewRequest("GET", proxyURL(kitePath), nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(401))
		})
	})

	Describe("Test KubeArchive endpoint", func() {
		kubearchivePath := kubearchiveEndpoint.BasePath

		BeforeEach(func() {
			if epModes.KubeArchive == modeSkip {
				Skip("KubeArchive endpoint not enabled in Konflux CR")
			}
		})

		It("should proxy authenticated requests with impersonation headers", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			if epModes.KubeArchive == modeEcho {
				// Poll to tolerate late service-serving cert projection on
				// OpenShift, matching the Watson test pattern.
				var lastHeaders map[string][]string
				Eventually(func(g Gomega) {
					lastHeaders = echoGetG(g, kubearchivePath+"echo", token)
					g.Expect(lastHeaders).To(HaveKey("Authorization"),
						"echo should receive Authorization header with kube_token")
					g.Expect(lastHeaders["Authorization"][0]).To(HavePrefix("Bearer "))
					g.Expect(lastHeaders).To(HaveKey("Impersonate-User"),
						"echo should receive Impersonate-User header")
					g.Expect(lastHeaders["Impersonate-User"][0]).To(Equal(expectedImpersonateUser()))
					g.Expect(lastHeaders).To(HaveKey("Impersonate-Group"),
						"echo should receive Impersonate-Group header")
					g.Expect(lastHeaders["Impersonate-Group"]).To(Equal([]string{"system:authenticated"}),
						"Impersonate-Group must be only system:authenticated")
				}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			} else {
				expectEndpointRouted(kubearchivePath, token)
			}
		})

		It("should reject unauthenticated requests", func() {
			request, err := http.NewRequest("GET", proxyURL(kubearchivePath), nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(401))
		})
	})

	Describe("Test Watson chatbot endpoint", func() {
		watsonPath := watsonEndpoint.BasePath

		BeforeEach(func() {
			if epModes.Watson == modeSkip {
				Skip("Watson endpoint not enabled in Konflux CR")
			}
		})

		It("should proxy authenticated requests with Basic auth header", func() {
			token, err := ExtractToken(proxyClient)
			Expect(err).NotTo(HaveOccurred())

			if epModes.Watson == modeEcho {
				expectedBasic := base64.StdEncoding.EncodeToString([]byte("apikey:" + watsonTestAPIKey))

				// The watson-config Secret is projected as a volume and cached by
				// file_watcher. Kubernetes may take up to ~60s to project a new
				// Secret, so we poll until the auth value is populated.
				var lastHeaders map[string][]string
				Eventually(func(g Gomega) {
					lastHeaders = echoGetG(g, watsonPath+"echo", token)
					g.Expect(lastHeaders).To(HaveKey("Authorization"))
					g.Expect(lastHeaders["Authorization"]).To(HaveLen(1))
					g.Expect(lastHeaders["Authorization"][0]).To(Equal("Basic "+expectedBasic),
						"Authorization should be Basic auth derived from watson API key")
				}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

				Expect(lastHeaders).NotTo(HaveKey("Impersonate-User"),
					"watson endpoint should NOT use impersonation")
				Expect(lastHeaders).NotTo(HaveKey("X-Auth-Request-Groups"),
					"watson endpoint must not forward identity-provider groups")
			} else {
				expectEndpointRouted(watsonPath, token)
			}
		})

		It("should reject unauthenticated requests", func() {
			request, err := http.NewRequest("GET", proxyURL(watsonPath), nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(401))
		})
	})

	Describe("Test runtime-config.js", func() {
		It("should serve runtime-config.js as static content", func() {
			request, err := http.NewRequest("GET", proxyURL("/runtime-config.js"), nil)
			Expect(err).NotTo(HaveOccurred())

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200),
				"runtime-config.js should be served as static content")

			content := string(body)
			Expect(content).To(ContainSubstring("window.KONFLUX_RUNTIME"),
				"runtime-config.js should define window.KONFLUX_RUNTIME")
		})

		It("should reflect runtime config values from the Konflux CR", func() {
			ctx := context.Background()
			konflux := &konfluxv1alpha1.Konflux{}
			Expect(proxyClient.Get(ctx, crclient.ObjectKey{Name: konfluxCRName}, konflux)).To(Succeed())

			var runtimeConfig *konfluxv1alpha1.RuntimeConfigSpec
			if konflux.Spec.KonfluxUI != nil && konflux.Spec.KonfluxUI.Spec != nil {
				runtimeConfig = konflux.Spec.KonfluxUI.Spec.RuntimeConfig
			}

			request, err := http.NewRequest("GET", proxyURL("/runtime-config.js"), nil)
			Expect(err).NotTo(HaveOccurred())

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200))

			content := string(body)

			if runtimeConfig == nil {
				By("runtimeConfig is nil — expecting only the empty initializer")
				Expect(content).To(ContainSubstring("window.KONFLUX_RUNTIME = window.KONFLUX_RUNTIME || {};"))
				return
			}

			for key, value := range runtimeConfig.All {
				propName := strings.TrimPrefix(key, "RUNTIME_")
				Expect(content).To(ContainSubstring(
					fmt.Sprintf(`window.KONFLUX_RUNTIME["%s"] = "%s";`, propName, value)),
					"runtime-config.js should contain property %s=%s from the CR", propName, value)
			}
		})
	})
})

type echoResponseBody struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

func expectEndpointRouted(path, token string) {
	GinkgoHelper()
	// Poll to tolerate transient reverse-proxy 5xx (e.g. OpenShift
	// service-serving cert not yet trusted by Caddy: x509 unknown authority → 502).
	Eventually(func(g Gomega) {
		request, err := http.NewRequest("GET", proxyURL(path), nil)
		g.Expect(err).NotTo(HaveOccurred())
		if err != nil {
			return
		}
		request.Header.Set("Authorization", "Bearer "+token)

		response, err := proxyHTTPClient.Do(request)
		g.Expect(err).NotTo(HaveOccurred())
		if err != nil {
			return
		}
		defer response.Body.Close()

		body, err := io.ReadAll(response.Body)
		g.Expect(err).NotTo(HaveOccurred())

		g.Expect(response.StatusCode).To(BeNumerically("<", 500),
			"backend at %s returned a server error (HTTP %d)", path, response.StatusCode)
		g.Expect(string(body)).NotTo(HavePrefix("<!doctype html>"),
			"expected a backend service response, got the SPA HTML fallback — proxy may not be routing to the endpoint")
	}).WithTimeout(proxyAPITransientRetryTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
}

// echoGetG performs a GET against the echo server and returns the echoed
// headers. It uses the provided Gomega instance for all assertions so that
// callers inside an Eventually block get soft failures (retries) instead of
// hard Ginkgo panics.
func echoGetG(g Gomega, path, token string) map[string][]string {
	request, err := http.NewRequest("GET", proxyURL(path), nil)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return nil
	}
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := proxyHTTPClient.Do(request)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return nil
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return nil
	}
	g.Expect(response.StatusCode).To(Equal(200), "echo request failed: %s", string(body))

	var echo echoResponseBody
	g.Expect(json.Unmarshal(body, &echo)).To(Succeed(), "failed to parse echo response: %s", string(body))
	return echo.Headers
}

// proxyGETStatusAndBodyG performs an authenticated GET for use inside Eventually.
func proxyGETStatusAndBodyG(g Gomega, path, token string) (int, []byte) {
	GinkgoHelper()
	request, err := http.NewRequest("GET", proxyURL(path), nil)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return 0, nil
	}
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := proxyHTTPClient.Do(request)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return 0, nil
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	g.Expect(err).NotTo(HaveOccurred())
	if err != nil {
		return 0, nil
	}
	return response.StatusCode, body
}

// expectImpersonateSAR asserts whether user may impersonate resource/name.
func expectImpersonateSAR(ctx context.Context, user, resource, name string, allowed bool) {
	GinkgoHelper()
	sar := &authv1.SubjectAccessReview{
		Spec: authv1.SubjectAccessReviewSpec{
			User: user,
			ResourceAttributes: &authv1.ResourceAttributes{
				Verb:     "impersonate",
				Group:    "",
				Resource: resource,
				Name:     name,
			},
		},
	}
	Expect(proxyClient.Create(ctx, sar)).To(Succeed())
	Expect(sar.Status.Allowed).To(Equal(allowed),
		"impersonate %s/%s for %s: allowed=%v reason=%s", resource, name, user, sar.Status.Allowed, sar.Status.Reason)
}

// namespaceListContains reports whether a Kubernetes NamespaceList JSON body
// includes a namespace with the given name. Unmarshal failures fail soft via g.
func namespaceListContains(g Gomega, body []byte, name string) bool {
	GinkgoHelper()
	var nsList struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	g.Expect(json.Unmarshal(body, &nsList)).To(Succeed(),
		"expected NamespaceList JSON, got: %s", string(body))
	for _, item := range nsList.Items {
		if item.Metadata.Name == name {
			return true
		}
	}
	return false
}

func echoGet(path, token string) map[string][]string {
	return echoGetG(Default, path, token)
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	IdToken     string `json:"id_token"`
}

func CreateHeaderFromSecret(secret *v1.Secret) (string, error) {
	encodedSecret, exists := secret.Data["client-secret"]
	if !exists {
		return "", fmt.Errorf("client-secret not found in secret")
	}
	decodedSecret := string(encodedSecret)
	header := base64.StdEncoding.EncodeToString([]byte("oauth2-proxy:" + decodedSecret))
	return header, nil
}

func GetIdToken(header string) (string, error) {
	return GetIdTokenForUser(header, userName, password)
}

func ExtractToken(client crclient.Client) (string, error) {
	return ExtractTokenForUser(client, userName, password)
}

func ExtractTokenForUser(client crclient.Client, user, pass string) (string, error) {
	if token, ok := proxyIDTokenForUser(user); ok {
		logProxyAuthMethod(proxyAuthMethodFromEnv())
		return token, nil
	}

	if isProxyOpenShiftAuth() {
		return "", fmt.Errorf(
			"%s=%s but no id_token available for %q (dex password grant disabled in openshift mode)",
			envProxyAuth, proxyAuthOpenShift, user,
		)
	}

	logProxyAuthMethod(proxyAuthMethodDexPasswordGrant)

	namespace := "dex"
	ns := &v1.Namespace{}
	err := client.Get(context.TODO(), crclient.ObjectKey{Name: "dex"}, ns)
	if err != nil {
		namespace = "konflux-ui"
	}

	secret := &v1.Secret{}
	err = client.Get(context.TODO(), crclient.ObjectKey{Namespace: namespace, Name: "oauth2-proxy-client-secret"}, secret)
	if err != nil {
		return "", err
	}
	header, err := CreateHeaderFromSecret(secret)
	if err != nil {
		return "", err
	}
	return GetIdTokenForUser(header, user, pass)
}

func GetIdTokenForUser(header, user, pass string) (string, error) {
	formData := url.Values{}
	formData.Add("grant_type", "password")
	formData.Add("scope", "openid profile email groups")
	formData.Add("username", user)
	formData.Add("password", pass)

	request, err := http.NewRequest("POST", tokenURL, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Basic "+header)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := proxyHTTPClient.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp TokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", err
	}
	return tokenResp.IdToken, nil
}

// GetIdTokenForPublicClient obtains a Dex ID token using the resource-owner
// password grant with a public client (no client secret). Kind/local only.
func GetIdTokenForPublicClient(clientID, user, pass string) (string, error) {
	formData := url.Values{}
	formData.Add("grant_type", "password")
	formData.Add("client_id", clientID)
	formData.Add("scope", "openid profile email groups")
	formData.Add("username", user)
	formData.Add("password", pass)

	request, err := http.NewRequest("POST", tokenURL, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := proxyHTTPClient.Do(request)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("dex token endpoint returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w (%s)", err, string(body))
	}
	if tokenResp.IdToken == "" {
		return "", fmt.Errorf("dex token response missing id_token: %s", string(body))
	}
	return tokenResp.IdToken, nil
}

// jwtAudienceContains reports whether the JWT aud claim is want or includes want.
func jwtAudienceContains(idToken, want string) (bool, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return false, fmt.Errorf("id token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false, fmt.Errorf("decode JWT payload: %w", err)
	}
	var claims struct {
		Aud json.RawMessage `json:"aud"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return false, fmt.Errorf("unmarshal JWT claims: %w", err)
	}
	var asString string
	if err := json.Unmarshal(claims.Aud, &asString); err == nil {
		return asString == want, nil
	}
	var asList []string
	if err := json.Unmarshal(claims.Aud, &asList); err == nil {
		for _, a := range asList {
			if a == want {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("unexpected aud claim %s", string(claims.Aud))
}
