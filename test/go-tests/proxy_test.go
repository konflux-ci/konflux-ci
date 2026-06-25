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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
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

	Describe("Test group-based RBAC", Label("proxy-dex"), func() {
		It("should grant access to namespaces the user's groups are bound to", func() {
			token, err := ExtractTokenForUser(proxyClient, "user1@konflux.dev", "password")
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
		})

		It("should pass groups from ID token as Impersonate-Group headers", func() {
			token, err := ExtractTokenForUser(proxyClient, "user1@konflux.dev", "password")
			Expect(err).NotTo(HaveOccurred())

			sarBody := `{"apiVersion":"authorization.k8s.io/v1","kind":"SelfSubjectAccessReview","spec":{"resourceAttributes":{"verb":"list","resource":"namespaces"}}}`
			request, err := http.NewRequest("POST",
				proxyURL("/api/k8s/apis/authorization.k8s.io/v1/selfsubjectaccessreviews"),
				strings.NewReader(sarBody))
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			expectProxyHTTPStatus(response, body, http.StatusCreated,
				"/api/k8s/apis/authorization.k8s.io/v1/selfsubjectaccessreviews")
		})
	})

	Describe("Test user with no IdP groups", Label("proxy-dex"), func() {
		It("should succeed when the user has no IdP groups", func() {
			token, err := ExtractTokenForUser(proxyClient, "user2@konflux.dev", "password")
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
				headers := echoGet(kitePath+"echo", token)
				Expect(headers).To(HaveKey("Authorization"),
					"echo should receive Authorization header with kube_token")
				Expect(headers["Authorization"]).To(HaveLen(1))
				Expect(headers["Authorization"][0]).To(HavePrefix("Bearer "),
					"Authorization should be a Bearer token (kube SA token)")
				Expect(headers).To(HaveKey("Impersonate-User"),
					"echo should receive Impersonate-User header")
				Expect(headers["Impersonate-User"][0]).To(Equal(expectedImpersonateUser()),
					"Impersonate-User should match the authenticated user")
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
				headers := echoGet(kubearchivePath+"echo", token)
				Expect(headers).To(HaveKey("Authorization"),
					"echo should receive Authorization header with kube_token")
				Expect(headers["Authorization"][0]).To(HavePrefix("Bearer "))
				Expect(headers).To(HaveKey("Impersonate-User"),
					"echo should receive Impersonate-User header")
				Expect(headers["Impersonate-User"][0]).To(Equal(expectedImpersonateUser()))
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

			expectedBasic := base64.StdEncoding.EncodeToString([]byte("apikey:" + watsonTestAPIKey))

			var lastHeaders map[string][]string
			Eventually(func(g Gomega) {
				lastHeaders = echoGet(watsonPath+"echo", token)
				g.Expect(lastHeaders).To(HaveKey("Authorization"))
				g.Expect(lastHeaders["Authorization"]).To(HaveLen(1))
				g.Expect(lastHeaders["Authorization"][0]).To(Equal("Basic "+expectedBasic),
					"Authorization should be Basic auth derived from watson API key")
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

			Expect(lastHeaders).NotTo(HaveKey("Impersonate-User"),
				"watson endpoint should NOT use impersonation")
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
})

type echoResponseBody struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
}

func expectEndpointRouted(path, token string) {
	GinkgoHelper()
	request, err := http.NewRequest("GET", proxyURL(path), nil)
	Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := proxyHTTPClient.Do(request)
	Expect(err).NotTo(HaveOccurred())
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	Expect(err).NotTo(HaveOccurred())

	Expect(string(body)).NotTo(HavePrefix("<!doctype html>"),
		"expected a backend service response, got the SPA HTML fallback — proxy may not be routing to the endpoint")
}

func echoGet(path, token string) map[string][]string {
	request, err := http.NewRequest("GET", proxyURL(path), nil)
	Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := proxyHTTPClient.Do(request)
	Expect(err).NotTo(HaveOccurred())
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	Expect(err).NotTo(HaveOccurred())
	Expect(response.StatusCode).To(Equal(200), "echo request failed: %s", string(body))

	var echo echoResponseBody
	Expect(json.Unmarshal(body, &echo)).To(Succeed(), "failed to parse echo response: %s", string(body))
	return echo.Headers
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
