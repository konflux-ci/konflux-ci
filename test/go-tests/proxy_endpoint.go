package go_tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const konfluxCRName = "konflux"

var konfluxGVR = schema.GroupVersionResource{
	Group:    "konflux.konflux-ci.dev",
	Version:  "v1alpha1",
	Resource: "konfluxes",
}

var (
	proxyHome       string
	tokenURL        string
	proxyHTTPClient *http.Client
	proxyK8sClient  *kubernetes.Clientset
)

var _ = BeforeSuite(func() {
	var err error
	proxyK8sClient, err = CreateK8sClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(proxyK8sClient).NotTo(BeNil())

	proxyHTTPClient = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	readyTimeout := konfluxReadyTimeout()
	pollInterval := 2 * time.Second

	dynamicClient, err := createDynamicClient()
	Expect(err).NotTo(HaveOccurred())

	ctx := context.Background()

	Eventually(func(g Gomega) {
		konflux, getErr := dynamicClient.Resource(konfluxGVR).Get(ctx, konfluxCRName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(HaveOccurred())
		g.Expect(konfluxReady(konflux)).To(BeTrue(), konfluxReadyMessage(konflux))

		baseURL := strings.TrimSpace(os.Getenv("KONFLUX_PROXY_URL"))
		if baseURL == "" {
			var found bool
			baseURL, found, getErr = konfluxUIURL(konflux)
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue(), "expected konflux/konflux status.uiURL to be set once Ready")
			g.Expect(baseURL).NotTo(BeEmpty())
		}
		proxyHome = strings.TrimRight(baseURL, "/")
		tokenURL = proxyHome + "/idp/token"
	}).WithTimeout(readyTimeout).WithPolling(pollInterval).Should(Succeed())

	healthTimeout := 5 * time.Minute
	if override := os.Getenv("KONFLUX_PROXY_HEALTH_TIMEOUT"); override != "" {
		if parsed, parseErr := time.ParseDuration(override); parseErr == nil {
			healthTimeout = parsed
		}
	}

	Eventually(func(g Gomega) {
		resp, doErr := proxyHTTPClient.Get(proxyHome + "/health")
		g.Expect(doErr).NotTo(HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}).WithTimeout(healthTimeout).WithPolling(pollInterval).Should(Succeed())
})

func konfluxReadyTimeout() time.Duration {
	for _, key := range []string{"KONFLUX_READY_TIMEOUT", "E2E_KONFLUX_READY_TIMEOUT"} {
		if raw := os.Getenv(key); raw != "" {
			if parsed, err := time.ParseDuration(raw); err == nil {
				return parsed
			}
		}
	}
	return 5 * time.Minute
}

func createDynamicClient() (dynamic.Interface, error) {
	config, err := restConfigFromKubeconfig()
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(config)
}

func konfluxReady(obj *unstructured.Unstructured) bool {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" && cond["status"] == "True" {
			return true
		}
	}
	return false
}

func konfluxReadyMessage(obj *unstructured.Unstructured) string {
	conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if err != nil || !found {
		return "konflux Ready condition not found in status"
	}
	for _, item := range conditions {
		cond, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "Ready" {
			return fmt.Sprintf("konflux Ready=%v reason=%v message=%v",
				cond["status"], cond["reason"], cond["message"])
		}
	}
	return "konflux Ready condition not found in status"
}

func konfluxUIURL(obj *unstructured.Unstructured) (string, bool, error) {
	return unstructured.NestedString(obj.Object, "status", "uiURL")
}

func proxyURL(path string) string {
	return proxyHome + path
}

func proxyWebSocketURL(path string) string {
	return strings.Replace(proxyHome, "https://", "wss://", 1) + path
}
