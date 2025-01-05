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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var tokenUrl = "https://localhost:9443/idp/token"
var userName = "user2@konflux.dev"
var password = "password"

var _ = Describe("Test Proxy endpoints", func() {
	// Create insecure http client (skip TLS verification)
	customTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Transport: customTransport,
	}
	var home = "https://localhost:9443"
	DescribeTable("Test endpoints without token", func(url string, expectedStatus int) {
		// Create a GET request
		request, err := http.NewRequest("GET", url, nil)
		Expect(err).NotTo(HaveOccurred())
		// Send the request
		response, err := client.Do(request)
		Expect(err).NotTo(HaveOccurred())
		Expect(response.StatusCode).To(Equal(expectedStatus))
	},
		Entry("Home", home, 200),
		Entry("Health", home+"/health", 200),
		Entry("Secrets",
			home+"/api/k8s/workspaces/user-ns2/api/v1/namespaces/user-ns2/secrets", 401),
		Entry("Release",
			home+"/api/k8s/workspaces/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/releaseplans", 401),
		Entry("Applications",
			home+"/api/k8s/workspaces/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/applications", 401),
		Entry("Workspaces",
			home+"/api/k8s/apis/toolchain.dev.openshift.com/v1alpha1/workspaces", 401),
	)
	k8sClient, err := CreateK8sClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	token, err := ExtractToken(k8sClient)
	Expect(err).NotTo(HaveOccurred())
	Expect(token).ToNot(BeEmpty())

	DescribeTable("Test endpoints with token",
		func(url string, expectedStatus int) {
			// Create a Get request
			request, err := http.NewRequest("GET", url, nil)
			Expect(err).NotTo(HaveOccurred())
			// Add authorization header to the request
			request.Header.Set("Authorization", "Bearer "+token)
			// Send the request
			response, err := client.Do(request)
			Expect(err).NotTo(HaveOccurred())
			Expect(response.StatusCode).To(Equal(expectedStatus))
		},

		Entry("Home", home, 200),
		Entry("Health", home+"/health", 200),
		Entry("Secrets",
			home+"/api/k8s/workspaces/user-ns2/api/v1/namespaces/user-ns2/secrets", 200),
		Entry("Release",
			home+"/api/k8s/workspaces/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/releaseplans", 200),
		Entry("Applications",
			home+"/api/k8s/workspaces/user-ns2/apis/appstudio.redhat.com/v1alpha1/namespaces/user-ns2/applications", 200),
		Entry("Workspaces",
			home+"/api/k8s/apis/toolchain.dev.openshift.com/v1alpha1/workspaces", 200),
	)
})

// A struct to hold the response of a token request
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
	IdToken     string `json:"id_token"`
}

// Create a header from a given secret.
// It takes a pointer to a Kubernetes Secret as input and returns a string representing the header.
// If the client-secret is not found in the secret, it returns an error
func CreateHeaderFromSecret(secret *v1.Secret) (string, error) {

	encodedSecret, exists := secret.Data["client-secret"]
	if !exists {
		return "", fmt.Errorf("client-secret not found in secret")
	}
	decodedSecret := string(encodedSecret)
	header := base64.StdEncoding.EncodeToString([]byte("oauth2-proxy:" + decodedSecret))
	return header, nil
}

// Retrieve the id_token from the tokenUrl using the provided username, password, and header.
func GetIdToken(header string) (string, error) {
	var tokenResp TokenResponse
	// Build the Post request to retrieve the id_token
	formData := url.Values{}
	formData.Add("grant_type", "password")
	formData.Add("scope", "openid profile email")
	formData.Add("username", userName)
	formData.Add("password", password)

	request, err := http.NewRequest("POST", tokenUrl, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return " ", err
	}
	request.Header.Set("Authorization", "Basic "+header)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	customTransport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // Skip TLS verification (insecure!)
	}
	client := &http.Client{Transport: customTransport}
	// send the request
	resp, err := client.Do(request)
	if err != nil {
		return " ", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	// Unmarshal the response body and send the id_token
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return " ", err
	}
	return tokenResp.IdToken, nil
}

func ExtractToken(k8sClient *kubernetes.Clientset) (string, error) {
	// Fetch the secret from k8s
	secret, err := k8sClient.CoreV1().Secrets("dex").Get(context.TODO(), "oauth2-proxy-client-secret", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	// Create a header from the extracted secret
	header, err := CreateHeaderFromSecret(secret)
	if err != nil {
		return "", err
	}
	// Get the id_token to use in our requests
	token, err := GetIdToken(header)
	if err != nil {
		return "", err
	}
	return token, nil
}
