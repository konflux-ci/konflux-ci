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
	"net/http"
	"net/url"
	"time"

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
	type rejectedBearer struct {
		name string
		mint func() string
	}
	type authenticatedRoute struct {
		name string
		kind string
		path string
	}
	bearers := []rejectedBearer{
		{"opaque string", func() string { return "this-is-not-a-jwt" }},
		{"alg none JWT", func() string { return forgedAlgNoneJWT(dex.CLIClientID) }},
		{"RS256 JWT with cli audience", func() string { return forgedRS256JWT(dex.CLIClientID) }},
		{"RS256 JWT with oauth2-proxy audience", func() string { return forgedRS256JWT(dex.OAuth2ProxyClientID) }},
		{"HS256 JWT", func() string { return forgedHS256JWT(dex.CLIClientID) }},
	}
	routes := []authenticatedRoute{
		{"kube API", "get", proxyNamespacesPath},
		{"websocket", "websocket", proxyTektonWSPath("pipelineruns") + "?watch=true"},
		{"tekton results", "get", proxyResultsPath},
		{"kite", "kite", kiteEndpoint.BasePath},
		{"kubearchive", "kubearchive", kubearchiveEndpoint.BasePath},
		{"watson", "watson", watsonEndpoint.BasePath},
	}
	entries := make([]TableEntry, 0, len(bearers)*len(routes))
	for _, bearer := range bearers {
		for _, route := range routes {
			entries = append(entries, Entry(bearer.name+" on "+route.name, bearer.mint, route.kind, route.path))
		}
	}

	DescribeTable("rejects credentials Dex did not issue on authenticated routes",
		func(mint func() string, kind, path string) {
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
			token := mint()
			if kind == "websocket" {
				expectProxyWebSocketStatus(path, http.Header{
					"Authorization": []string{"Bearer " + token},
					"Origin":        []string{proxyHome},
				}, http.StatusForbidden)
				return
			}
			expectProxyGETWithBearer(path, token, http.StatusForbidden)
		},
		entries,
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

		requestPath := proxyNamespacesPath + "?access_token=" + url.QueryEscape(token)
		reportPath := proxyNamespacesPath + "?access_token=REDACTED"
		expectProxyGETSecret(requestPath, reportPath, token, nil, http.StatusUnauthorized)
	})
})

// kubernetesServiceAccountToken asks the API server for a short-lived token
// for the proxy ServiceAccount. The proxy must reject it as a bearer.
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

// dexIssuer is the issuer claim oauth2-proxy expects for a Dex ID token.
func dexIssuer() string {
	return proxyHome + "/idp/"
}

// plausibleIDTokenClaims returns claims that look like a Dex ID token for aud.
// The signature is supplied separately and is not Dex's.
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

// forgedAlgNoneJWT returns an unsigned JWT with alg "none".
func forgedAlgNoneJWT(aud string) string {
	return unsignedJWT("none", plausibleIDTokenClaims(aud))
}

// forgedRS256JWT returns an RS256 JWT signed with a throwaway key.
// Call it from a running spec: it uses Expect, and GinkgoHelper reports failures there.
func forgedRS256JWT(aud string) string {
	GinkgoHelper()
	header, payload := signedJWTParts("RS256", plausibleIDTokenClaims(aud))
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())
	sum := sha256.Sum256([]byte(header + "." + payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	Expect(err).NotTo(HaveOccurred())
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// forgedHS256JWT returns an HS256 JWT signed with a key Dex does not have.
func forgedHS256JWT(aud string) string {
	GinkgoHelper()
	header, payload := signedJWTParts("HS256", plausibleIDTokenClaims(aud))
	mac := hmac.New(sha256.New, []byte("not-the-dex-signing-key"))
	_, _ = mac.Write([]byte(header + "." + payload))
	return header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// unsignedJWT returns header.payload. with an empty signature segment.
func unsignedJWT(alg string, claims map[string]any) string {
	header, payload := signedJWTParts(alg, claims)
	return header + "." + payload + "."
}

// signedJWTParts returns the base64url header and payload of a JWT.
func signedJWTParts(alg string, claims map[string]any) (string, string) {
	return b64JSON(map[string]any{"alg": alg, "typ": "JWT"}), b64JSON(claims)
}

// b64JSON returns the raw base64url encoding of v as JSON.
func b64JSON(v any) string {
	GinkgoHelper()
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return base64.RawURLEncoding.EncodeToString(raw)
}
