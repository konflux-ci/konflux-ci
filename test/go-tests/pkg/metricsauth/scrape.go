package metricsauth

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const scrapeRequestTimeout = 30 * time.Second

// ScrapeResult holds the HTTP response from a metrics scrape.
type ScrapeResult struct {
	StatusCode int
	Body       []byte
}

// ServiceAccountToken mints a token for the scraper service account.
func ServiceAccountToken(ctx context.Context, cfg *rest.Config, namespace, name string) (string, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", err
	}
	tr, err := clientset.CoreV1().ServiceAccounts(namespace).CreateToken(
		ctx, name, &authenticationv1.TokenRequest{
			Spec: authenticationv1.TokenRequestSpec{
				ExpirationSeconds: ptr.To(int64(3600)),
			},
		}, metav1.CreateOptions{},
	)
	if err != nil {
		return "", err
	}
	if tr.Status.Token == "" {
		return "", fmt.Errorf("empty token for serviceaccount %s/%s", namespace, name)
	}
	return tr.Status.Token, nil
}

// SecretToken reads a bearer token from an operand scrape Secret.
func SecretToken(ctx context.Context, c client.Reader, namespace, name, key string) (string, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, secret); err != nil {
		return "", err
	}
	token := secret.Data[key]
	if len(token) == 0 {
		return "", fmt.Errorf("secret %s/%s key %q is empty", namespace, name, key)
	}
	return string(token), nil
}

// ScrapeLocal GETs a metrics URL using a bearer token.
func ScrapeLocal(ctx context.Context, localURL, token, scheme string, tlsInsecureSkipVerify bool) (*ScrapeResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, localURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	var transport http.RoundTripper
	if scheme == "http" {
		transport = &http.Transport{}
	} else {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: tlsInsecureSkipVerify}, //nolint:gosec // controlled by catalog until CA wiring lands
		}
	}
	client := &http.Client{Timeout: scrapeRequestTimeout, Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &ScrapeResult{StatusCode: resp.StatusCode, Body: body}, nil
}

// ScrapeLocalHTTPS GETs an HTTPS metrics URL using a bearer token.
// tlsInsecureSkipVerify should be true until cert-manager CA verification is wired.
func ScrapeLocalHTTPS(ctx context.Context, localURL, token string, tlsInsecureSkipVerify bool) (*ScrapeResult, error) {
	return ScrapeLocal(ctx, localURL, token, "https", tlsInsecureSkipVerify)
}

// ValidatePrometheusText checks status and that the body looks like Prometheus exposition.
func ValidatePrometheusText(result *ScrapeResult, mustMatchAny []string) error {
	if result.StatusCode != http.StatusOK {
		snippet := string(result.Body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return fmt.Errorf("expected HTTP 200, got %d: %s", result.StatusCode, snippet)
	}
	body := string(result.Body)
	for _, sub := range mustMatchAny {
		if strings.Contains(body, sub) {
			return nil
		}
	}
	return fmt.Errorf("metrics body missing expected substrings: %v", mustMatchAny)
}

// LocalMetricsURL builds a URL for a port-forwarded metrics service.
func LocalMetricsURL(localPort int, path, scheme string) string {
	if scheme == "" {
		scheme = "https"
	}
	if path == "" {
		path = "/metrics"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return fmt.Sprintf("%s://127.0.0.1:%d%s", scheme, localPort, path)
}

// ServiceRef identifies a Kubernetes Service for port-forwarding.
type ServiceRef struct {
	Namespace string
	Name      string
	Port      int32
}

// PortForwarder forwards a local port to a service port.
type PortForwarder struct {
	stopCh    chan struct{}
	readyCh   chan struct{}
	localPort int
}

// StartPortForward listens on 127.0.0.1:0 and forwards to the service port via pod port-forward.
func StartPortForward(ctx context.Context, cfg *rest.Config, svc ServiceRef) (*PortForwarder, error) {
	pf, localPort, err := startServicePortForward(ctx, cfg, svc)
	if err != nil {
		return nil, err
	}
	return &PortForwarder{
		stopCh:    pf.stopCh,
		readyCh:   pf.readyCh,
		localPort: localPort,
	}, nil
}

// LocalPort returns the local TCP port for the forward.
func (p *PortForwarder) LocalPort() int {
	return p.localPort
}

// Close stops the port-forward.
func (p *PortForwarder) Close() {
	close(p.stopCh)
}

// WaitReady blocks until the port-forward is ready or ctx is cancelled.
func (p *PortForwarder) WaitReady(ctx context.Context) error {
	select {
	case <-p.readyCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
