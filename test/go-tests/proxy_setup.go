package go_tests

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	konfluxCRName              = "konflux"
	konfluxReadyWaitTimeout    = 5 * time.Minute
	konfluxHealthWaitTimeout   = 5 * time.Minute
	konfluxReadyPollInterval   = 2 * time.Second
	proxyHTTPRequestTimeout    = 30 * time.Second
)

var (
	proxyBase       *url.URL
	proxyHome       string
	tokenURL        string
	proxyHTTPClient *http.Client
	proxyClient     crclient.Client
)

var _ = BeforeSuite(func() {
	var err error
	proxyClient, err = NewClient()
	Expect(err).NotTo(HaveOccurred())
	Expect(proxyClient).NotTo(BeNil())

	proxyHTTPClient = &http.Client{
		Timeout: proxyHTTPRequestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	ctx := context.Background()

	Eventually(func(g Gomega) {
		konflux := &konfluxv1alpha1.Konflux{}
		getErr := proxyClient.Get(ctx, crclient.ObjectKey{Name: konfluxCRName}, konflux)
		g.Expect(getErr).NotTo(HaveOccurred())
		g.Expect(konflux.IsReady()).To(BeTrue(), konflux.ReadyConditionMessage())

		baseURL := strings.TrimSpace(os.Getenv("KONFLUX_PROXY_URL"))
		if baseURL == "" {
			baseURL = konflux.Status.UIURL
			g.Expect(baseURL).NotTo(BeEmpty(), "expected konflux/konflux status.uiURL to be set once Ready")
		}
		g.Expect(setProxyBase(baseURL)).NotTo(HaveOccurred())
	}).WithTimeout(konfluxReadyWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())

	Eventually(func(g Gomega) {
		resp, doErr := proxyHTTPClient.Get(proxyURL("/health"))
		g.Expect(doErr).NotTo(HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp.StatusCode).To(Equal(http.StatusOK))
	}).WithTimeout(konfluxHealthWaitTimeout).WithPolling(konfluxReadyPollInterval).Should(Succeed())
})

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
