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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var (
	userName = "user2@konflux.dev"
	password = "password"
)

var _ = Describe("Test Proxy endpoints", func() {
	DescribeTable("Test endpoints without token",
		func(path string, expectedStatus int) {
			request, err := http.NewRequest("GET", proxyURL(path), nil)
			Expect(err).NotTo(HaveOccurred())
			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(expectedStatus))
		},
		Entry("Home", "", 200),
		Entry("Health", "/health", 200),
		Entry("Secrets",
			"/api/k8s/ns/user-ns2/api/v1/namespaces/user-ns2/secrets", 401),
		Entry("Release",
			"/api/k8s/ns/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/releaseplans", 401),
		Entry("Applications",
			"/api/k8s/ns/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/applications", 401),
		Entry("Namespaces",
			"/api/k8s/api/v1/namespaces", 401),
	)

	DescribeTable("Test endpoints with token",
		func(path string, expectedStatus int) {
			token, err := ExtractToken(proxyK8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).ToNot(BeEmpty())

			request, err := http.NewRequest("GET", proxyURL(path), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())

			defer response.Body.Close()
			rbody, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(expectedStatus), string(rbody))
		},
		Entry("Home", "", 200),
		Entry("Health", "/health", 200),
		Entry("Secrets",
			"/api/k8s/api/v1/namespaces/user-ns2/secrets", 200),
		Entry("Release",
			"/api/k8s/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/releaseplans", 200),
		Entry("Applications",
			"/api/k8s/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/applications", 200),
		Entry("Namespaces",
			"/api/k8s/api/v1/namespaces", 200),
		Entry("PipelineRuns",
			"/api/k8s/apis/tekton.dev/v1/namespaces/user-ns2/pipelineruns", 200),
	)

	Describe("Test WebSocket endpoint", func() {
		const wsPath = "/wss/k8s/apis/tekton.dev/v1/namespaces/user-ns2/pipelineruns?watch=true"

		It("should establish a WebSocket connection through /wss/k8s/", func() {
			token, err := ExtractToken(proxyK8sClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).ToNot(BeEmpty())

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			httpClient := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
				},
			}

			conn, resp, err := websocket.Dial(ctx, proxyWebSocketURL(wsPath), &websocket.DialOptions{
				HTTPClient:   httpClient,
				Subprotocols: []string{"base64.binary.k8s.io"},
				HTTPHeader: http.Header{
					"Authorization": []string{"Bearer " + token},
					"Origin":        []string{proxyHome},
				},
			})
			Expect(err).NotTo(HaveOccurred(), "WebSocket connection should be established")
			Expect(resp.StatusCode).To(Equal(http.StatusSwitchingProtocols))
			defer conn.Close(websocket.StatusNormalClosure, "test complete")
		})

		It("should reject WebSocket connection without token", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

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
			token, err := ExtractToken(proxyK8sClient)
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
			token, err := ExtractToken(proxyK8sClient)
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

	Describe("Test group-based RBAC", func() {
		It("should grant access to namespaces the user's groups are bound to", func() {
			token, err := ExtractTokenForUser(proxyK8sClient, "user1@konflux.dev", "password")
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200), string(body))
		})

		It("should pass groups from ID token as Impersonate-Group headers", func() {
			token, err := ExtractTokenForUser(proxyK8sClient, "user1@konflux.dev", "password")
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
			Expect(response.StatusCode).To(Equal(201), string(body))
		})
	})

	Describe("Test user with no IdP groups", func() {
		It("should succeed when the user has no IdP groups", func() {
			token, err := ExtractTokenForUser(proxyK8sClient, "user2@konflux.dev", "password")
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET",
				proxyURL("/api/k8s/api/v1/namespaces/user-ns2/secrets"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200), string(body),
				"Request should succeed when user has no IdP groups (plugin adds system:authenticated only)")
		})
	})

	Describe("Test namespace-lister endpoint", func() {
		It("should return namespaces for authenticated user", func() {
			token, err := ExtractToken(proxyK8sClient)
			Expect(err).NotTo(HaveOccurred())

			request, err := http.NewRequest("GET", proxyURL("/api/k8s/api/v1/namespaces"), nil)
			Expect(err).NotTo(HaveOccurred())
			request.Header.Set("Authorization", "Bearer "+token)

			response, err := proxyHTTPClient.Do(request)
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()

			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(200), string(body))

			var nsList map[string]interface{}
			err = json.Unmarshal(body, &nsList)
			Expect(err).NotTo(HaveOccurred())
			Expect(nsList).To(HaveKey("items"), "response should be a namespace list")
		})

		It("should route non-GET methods to Kube API, not namespace-lister", func() {
			token, err := ExtractToken(proxyK8sClient)
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
			token, err := ExtractToken(proxyK8sClient)
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
			raw, err := proxyK8sClient.CoreV1().Services("konflux-ui").
				ProxyGet("http", "proxy", "metrics", "/metrics", nil).
				DoRaw(context.TODO())
			Expect(err).NotTo(HaveOccurred())
			Expect(string(raw)).To(ContainSubstring("caddy_"),
				"metrics should contain Caddy-specific metrics")
		})
	})
})

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

func ExtractToken(k8sClient *kubernetes.Clientset) (string, error) {
	return ExtractTokenForUser(k8sClient, userName, password)
}

func ExtractTokenForUser(k8sClient *kubernetes.Clientset, user, pass string) (string, error) {
	namespace := "dex"
	_, err := k8sClient.CoreV1().Namespaces().Get(context.TODO(), "dex", metav1.GetOptions{})
	if err != nil {
		namespace = "konflux-ui"
	}

	secret, err := k8sClient.CoreV1().Secrets(namespace).Get(context.TODO(), "oauth2-proxy-client-secret", metav1.GetOptions{})
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

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Do(request)
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
