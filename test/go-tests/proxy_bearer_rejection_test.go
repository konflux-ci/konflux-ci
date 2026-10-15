package go_tests

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	proxyNamespacesPath = "/api/k8s/api/v1/namespaces"
	proxyResultsPath    = "/api/k8s/plugins/tekton-results/apis/results.tekton.dev/v1alpha2/parents/-/results"
)

var _ = Describe("Test bearer credentials", func() {
	DescribeTable("rejects an Authorization header Dex did not issue",
		func(mint func() string) {
			expectProxyGETWithBearer(proxyNamespacesPath, mint(), http.StatusForbidden)
		},
		Entry("opaque string", func() string { return "this-is-not-a-jwt" }),
		Entry("alg none JWT", func() string { return forgedAlgNoneJWT(dex.CLIClientID) }),
		Entry("RS256 JWT with cli audience", func() string { return forgedRS256JWT(dex.CLIClientID) }),
		Entry("RS256 JWT with oauth2-proxy audience", func() string { return forgedRS256JWT(dex.OAuth2ProxyClientID) }),
		Entry("HS256 JWT", func() string { return forgedHS256JWT(dex.CLIClientID) }),
	)

	It("rejects an empty bearer", func() {
		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer")
		}, http.StatusForbidden)
	})

	It("rejects basic auth", func() {
		raw := base64.StdEncoding.EncodeToString([]byte("alice:secret"))
		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			req.Header.Set("Authorization", "Basic "+raw)
		}, http.StatusForbidden)
	})

	It("rejects a Kubernetes service account token", func() {
		token, err := kubernetesServiceAccountToken(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())
		expectProxyGETWithBearer(proxyNamespacesPath, token, http.StatusForbidden)
	})

	DescribeTable("rejects a forged RS256 token on authenticated routes",
		func(kind, path string) {
			switch kind {
			case "kite":
				if epModes.Kite == modeSkip {
					Skip("Kite endpoint not enabled in Konflux CR")
				}
			case "kubearchive":
				if epModes.KubeArchive == modeSkip {
					Skip("KubeArchive endpoint not enabled in Konflux CR")
				}
			case "watson":
				if epModes.Watson == modeSkip {
					Skip("Watson endpoint not enabled in Konflux CR")
				}
			}
			token := forgedRS256JWT(dex.CLIClientID)
			if kind == "websocket" {
				expectProxyWebSocketStatus(path, http.Header{
					"Authorization": []string{"Bearer " + token},
					"Origin":        []string{proxyHome},
				}, http.StatusForbidden)
				return
			}
			expectProxyGETWithBearer(path, token, http.StatusForbidden)
		},
		Entry("kube API", "get", proxyNamespacesPath),
		Entry("websocket", "websocket", proxyTektonWSPath("pipelineruns")+"?watch=true"),
		Entry("tekton results", "get", proxyResultsPath),
		Entry("kite", "kite", kiteEndpoint.BasePath),
		Entry("kubearchive", "kubearchive", kubearchiveEndpoint.BasePath),
		Entry("watson", "watson", watsonEndpoint.BasePath),
	)

	It("rejects a garbage session cookie", func() {
		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			req.AddCookie(&http.Cookie{Name: proxySessionCookieName, Value: "not-a-session"})
		}, http.StatusUnauthorized)
	})

	It("rejects a forged bearer sent with a valid session cookie", func() {
		cookies, err := obtainProxySessionCookie(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(cookies).NotTo(BeEmpty())

		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
		}, http.StatusOK)

		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			for _, cookie := range cookies {
				req.AddCookie(cookie)
			}
			req.Header.Set("Authorization", "Bearer "+forgedRS256JWT(dex.CLIClientID))
		}, http.StatusForbidden)
	})

	It("rejects a forged bearer that also spoofs X-Auth-Request-Email", func() {
		expectProxyGET(proxyNamespacesPath, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+forgedRS256JWT(dex.CLIClientID))
			req.Header.Set("X-Auth-Request-Email", "admin@example.com")
		}, http.StatusForbidden)
	})

	It("does not accept a Dex ID token from the query string", func() {
		token, err := ExtractToken(proxyClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())

		path := proxyNamespacesPath + "?access_token=" + url.QueryEscape(token)
		expectProxyGET(path, nil, http.StatusUnauthorized)
	})
})

func expectProxyGET(path string, set func(*http.Request), expected int) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		req, err := http.NewRequest(http.MethodGet, proxyURL(path), nil)
		g.Expect(err).NotTo(HaveOccurred())
		if set != nil {
			set(req)
		}

		resp, err := proxyHTTPClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		g.Expect(err).NotTo(HaveOccurred())

		if proxyTransientHTTPStatus(resp.StatusCode) {
			g.Expect(resp.StatusCode).To(Equal(expected),
				"%s: transient HTTP %d, retrying", path, resp.StatusCode)
			return
		}
		g.Expect(resp.StatusCode).To(Equal(expected), proxyHTTPStatusMismatch(resp, body, expected, path))
	}).WithTimeout(proxyAPITransientRetryTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
}

func expectProxyWebSocketStatus(wsPath string, header http.Header, expected int) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		ctx, cancel := context.WithTimeout(context.Background(), proxyHTTPRequestTimeout)
		defer cancel()

		conn, resp, err := websocket.Dial(ctx, proxyWebSocketURL(wsPath), &websocket.DialOptions{
			HTTPClient:   proxyWebSocketHTTPClient,
			Subprotocols: []string{"base64.binary.k8s.io"},
			HTTPHeader:   header,
		})
		if resp != nil && proxyTransientHTTPStatus(resp.StatusCode) {
			proxyDrainResponseBody(resp)
			g.Expect(resp.StatusCode).To(Equal(expected),
				"%s: transient HTTP %d on WebSocket upgrade, retrying", wsPath, resp.StatusCode)
			return
		}
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "test complete")
		}
		g.Expect(err).To(HaveOccurred())
		g.Expect(resp).NotTo(BeNil())
		if resp != nil {
			g.Expect(resp.StatusCode).To(Equal(expected))
			proxyDrainResponseBody(resp)
		}
	}).WithTimeout(proxyAPITransientRetryTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
}

func kubernetesServiceAccountToken(ctx context.Context) (string, error) {
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name:      "proxy",
		Namespace: "konflux-ui",
	}}
	expiration := int64(600)
	tokenRequest := &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			ExpirationSeconds: &expiration,
		},
	}
	if err := proxyClient.SubResource("token").Create(ctx, sa, tokenRequest); err != nil {
		return "", err
	}
	return tokenRequest.Status.Token, nil
}

func dexIssuer() string {
	return proxyHome + "/idp/"
}

func plausibleIDTokenClaims(aud string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss":   dexIssuer(),
		"aud":   aud,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"email": userName,
		"sub":   userName,
	}
}

func forgedAlgNoneJWT(aud string) string {
	return unsignedJWT("none", plausibleIDTokenClaims(aud))
}

func forgedRS256JWT(aud string) string {
	header, payload := signedJWTParts("RS256", plausibleIDTokenClaims(aud))
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	sum := sha256.Sum256([]byte(header + "." + payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	Expect(err).NotTo(HaveOccurred())
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func forgedHS256JWT(aud string) string {
	header, payload := signedJWTParts("HS256", plausibleIDTokenClaims(aud))
	mac := hmac.New(sha256.New, []byte("not-the-dex-signing-key"))
	_, _ = mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func unsignedJWT(alg string, claims map[string]any) string {
	header, payload := signedJWTParts(alg, claims)
	return header + "." + payload + "."
}

func signedJWTParts(alg string, claims map[string]any) (string, string) {
	return b64JSON(map[string]any{"alg": alg, "typ": "JWT"}), b64JSON(claims)
}

func b64JSON(v any) string {
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return base64.RawURLEncoding.EncodeToString(raw)
}
