package go_tests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	htmlpkg "html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"

	v1 "k8s.io/api/core/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	openShiftOAuthScope        = "openid profile email groups"
	openShiftOAuthClientID     = "oauth2-proxy"
	openShiftOAuthMaxRedirects = 30
)

// openshiftHTTPDoer performs OAuth HTTP steps (injectable for tests).
type openshiftHTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// envOpenShiftPasswordSource reads password from CI env vars/files.
type envOpenShiftPasswordSource struct{}

func (envOpenShiftPasswordSource) OpenShiftPassword() (string, error) {
	if password := strings.TrimSpace(os.Getenv("OPENSHIFT_PASSWORD")); password != "" {
		return password, nil
	}
	if path := strings.TrimSpace(os.Getenv("KUBEADMIN_PASSWORD_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return strings.TrimSpace(strings.ReplaceAll(string(data), "\n", "")), nil
		}
	}
	if shared := strings.TrimSpace(os.Getenv("SHARED_DIR")); shared != "" {
		path := shared + "/kubeadmin-password"
		if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return strings.TrimSpace(strings.ReplaceAll(string(data), "\n", "")), nil
		}
	}
	return "", fmt.Errorf("openshift password required: set OPENSHIFT_PASSWORD, KUBEADMIN_PASSWORD_FILE, or SHARED_DIR/kubeadmin-password")
}

// kubeOAuth2ProxySecretSource loads oauth2-proxy-client-secret from dex or konflux-ui.
type kubeOAuth2ProxySecretSource struct {
	client    crclient.Client
	namespace string
}

func (k kubeOAuth2ProxySecretSource) OAuth2ProxyClientSecret(ctx context.Context) (string, error) {
	namespaces := []string{"dex", k.namespace}
	if k.namespace == "dex" {
		namespaces = []string{"dex"}
	}
	for _, ns := range namespaces {
		secret := &v1.Secret{}
		err := k.client.Get(ctx, crclient.ObjectKey{Namespace: ns, Name: "oauth2-proxy-client-secret"}, secret)
		if err != nil {
			continue
		}
		raw, ok := secret.Data["client-secret"]
		if !ok {
			return "", fmt.Errorf("client-secret not found in secret %s/oauth2-proxy-client-secret", ns)
		}
		return string(raw), nil
	}
	return "", fmt.Errorf("oauth2-proxy-client-secret not found in dex or %s", k.namespace)
}

// OpenShiftOAuthConfig holds inputs for the authorization-code flow.
type OpenShiftOAuthConfig struct {
	UIURL        string
	Username     string
	Password     string
	ClientSecret string
	Scope        string
	MaxRedirects int
}

// OpenShiftOAuthClient runs the browser-less OpenShift → Dex OAuth flow.
type OpenShiftOAuthClient struct {
	http openshiftHTTPDoer
}

func newOpenShiftOAuthClient(base *http.Client) (*OpenShiftOAuthClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := base.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client := &http.Client{
		Timeout:       base.Timeout,
		Transport:     transport,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &OpenShiftOAuthClient{http: client}, nil
}

func obtainOpenShiftProxyIDToken(ctx context.Context, baseHTTP *http.Client, kube crclient.Client, uiURL string) (string, error) {
	oauthHTTP, err := newOpenShiftOAuthClient(baseHTTP)
	if err != nil {
		return "", err
	}
	password, err := (envOpenShiftPasswordSource{}).OpenShiftPassword()
	if err != nil {
		return "", err
	}
	ns := strings.TrimSpace(os.Getenv("KONFLUX_UI_NAMESPACE"))
	if ns == "" {
		ns = "konflux-ui"
	}
	secret, err := (kubeOAuth2ProxySecretSource{client: kube, namespace: ns}).OAuth2ProxyClientSecret(ctx)
	if err != nil {
		return "", err
	}
	username := strings.TrimSpace(os.Getenv("OPENSHIFT_USERNAME"))
	if username == "" {
		username = "kubeadmin"
	}
	cfg := OpenShiftOAuthConfig{
		UIURL:        strings.TrimRight(strings.TrimSpace(uiURL), "/"),
		Username:     username,
		Password:     password,
		ClientSecret: secret,
		Scope:        openShiftOAuthScope,
		MaxRedirects: openShiftOAuthMaxRedirects,
	}
	return oauthHTTP.ObtainIDToken(ctx, cfg)
}

// ObtainIDToken completes the OAuth authorization-code flow and returns a Dex id_token.
func (c *OpenShiftOAuthClient) ObtainIDToken(ctx context.Context, cfg OpenShiftOAuthConfig) (string, error) {
	if cfg.Scope == "" {
		cfg.Scope = openShiftOAuthScope
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = openShiftOAuthMaxRedirects
	}
	redirectURI := cfg.UIURL + "/oauth2/callback"
	authCode, err := c.obtainAuthorizationCode(ctx, cfg, redirectURI)
	if err != nil {
		return "", err
	}
	return c.exchangeAuthorizationCode(ctx, cfg, redirectURI, authCode)
}

func (c *OpenShiftOAuthClient) obtainAuthorizationCode(ctx context.Context, cfg OpenShiftOAuthConfig, redirectURI string) (string, error) {
	start, err := url.Parse(cfg.UIURL + "/idp/auth/openshift")
	if err != nil {
		return "", err
	}
	q := start.Query()
	q.Set("client_id", openShiftOAuthClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", cfg.Scope)
	start.RawQuery = q.Encode()

	current := start.String()
	var pendingAuthorize string

	for i := 0; i < cfg.MaxRedirects; i++ {
		if code := queryParam(current, "code"); code != "" && strings.Contains(current, "/oauth2/callback") {
			return code, nil
		}

		status, location, body, err := c.doGET(ctx, current)
		if err != nil {
			return "", err
		}

		if status >= 300 && status < 400 && location != "" {
			if strings.Contains(location, "/oauth/authorize") {
				pendingAuthorize, err = resolveURL(current, location)
				if err != nil {
					return "", err
				}
			}
			current, err = resolveURL(current, location)
			if err != nil {
				return "", err
			}
			if code := queryParam(current, "code"); code != "" && strings.Contains(current, "/oauth2/callback") {
				return code, nil
			}
			if strings.Contains(current, "/login") {
				_, _, loginBody, err := c.doGET(ctx, current)
				if err != nil {
					return "", err
				}
				current, err = c.submitLoginAndFollow(ctx, current, loginBody, cfg, pendingAuthorize)
				if err != nil {
					return "", err
				}
			}
			continue
		}

		if status == http.StatusOK && strings.Contains(current, "/login") {
			current, err = c.submitLoginAndFollow(ctx, current, body, cfg, pendingAuthorize)
			if err != nil {
				return "", err
			}
			continue
		}

		if status == http.StatusOK && strings.Contains(current, "/authorize/approve") {
			current, err = c.submitGrant(ctx, current, body)
			if err != nil {
				return "", err
			}
			continue
		}

		return "", fmt.Errorf("unexpected OAuth response (http=%d, url=%s)", status, redactOAuthURL(current))
	}
	return "", fmt.Errorf("too many redirects during OpenShift OAuth flow")
}

func (c *OpenShiftOAuthClient) submitLoginAndFollow(ctx context.Context, pageURL string, body []byte, cfg OpenShiftOAuthConfig, pendingAuthorize string) (string, error) {
	action, csrf, err := parseLoginForm(pageURL, body)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("csrf", csrf)
	form.Set("username", cfg.Username)
	form.Set("password", cfg.Password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, location, _, err := c.doRequest(req)
	if err != nil {
		return "", err
	}
	if status >= 300 && status < 400 && location != "" {
		return resolveURL(action, location)
	}
	if pendingAuthorize != "" {
		return pendingAuthorize, nil
	}
	return "", fmt.Errorf("OpenShift login succeeded but no redirect target was found")
}

func (c *OpenShiftOAuthClient) submitGrant(ctx context.Context, pageURL string, body []byte) (string, error) {
	action, form, err := parseGrantForm(pageURL, body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, location, _, err := c.doRequest(req)
	if err != nil {
		return "", err
	}
	if status >= 300 && status < 400 && location != "" {
		return resolveURL(pageURL, location)
	}
	return "", fmt.Errorf("OpenShift grant approval succeeded but no redirect target was found")
}

func (c *OpenShiftOAuthClient) exchangeAuthorizationCode(ctx context.Context, cfg OpenShiftOAuthConfig, redirectURI, authCode string) (string, error) {
	tokenURL := cfg.UIURL + "/idp/token"
	auth := base64.StdEncoding.EncodeToString([]byte("oauth2-proxy:" + cfg.ClientSecret))
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
		Error   string `json:"error"`
		Desc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("token exchange failed: decode response: %w", err)
	}
	if tokenResp.IDToken != "" {
		return tokenResp.IDToken, nil
	}
	if tokenResp.Error != "" || tokenResp.Desc != "" {
		return "", fmt.Errorf("token exchange failed: error=%q description=%q", tokenResp.Error, tokenResp.Desc)
	}
	return "", fmt.Errorf("token exchange failed: no id_token in response (%d bytes)", len(body))
}

func (c *OpenShiftOAuthClient) doGET(ctx context.Context, rawURL string) (int, string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, "", nil, err
	}
	return c.doRequest(req)
}

func (c *OpenShiftOAuthClient) doRequest(req *http.Request) (int, string, []byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", nil, err
	}
	return resp.StatusCode, resp.Header.Get("Location"), body, nil
}

var (
	reFormAction   = regexp.MustCompile(`(?i)<form[^>]*action="([^"]*)"`)
	reInputCSRF    = regexp.MustCompile(`(?i)name="csrf"[^>]*value="([^"]*)"`)
	reHiddenInput  = regexp.MustCompile(`(?i)<input type="hidden" name="([^"]*)" value="([^"]*)"`)
	reCheckedScope = regexp.MustCompile(`(?i)<input type="checkbox"[^>]*checked[^>]*name="scope" value="([^"]*)"`)
	reApproveValue = regexp.MustCompile(`(?i)name="approve"[^>]*value="([^"]*)"`)
)

func parseLoginForm(pageURL string, body []byte) (actionURL, csrf string, err error) {
	pageHTML := string(body)
	actionMatch := reFormAction.FindStringSubmatch(pageHTML)
	csrfMatch := reInputCSRF.FindStringSubmatch(pageHTML)
	if len(actionMatch) < 2 || len(csrfMatch) < 2 {
		return "", "", fmt.Errorf("OpenShift login form not found at %s", redactOAuthURL(pageURL))
	}
	action := htmlpkg.UnescapeString(actionMatch[1])
	actionURL, err = resolveURL(pageURL, action)
	if err != nil {
		return "", "", err
	}
	if action == "/login" || action == "login" {
		base := pageURL
		if i := strings.Index(base, "?"); i >= 0 {
			base = base[:i]
		}
		actionURL = base
		if strings.Contains(pageURL, "?") {
			actionURL = pageURL
		}
	}
	return actionURL, htmlpkg.UnescapeString(csrfMatch[1]), nil
}

func parseGrantForm(pageURL string, body []byte) (actionURL string, form url.Values, err error) {
	pageHTML := string(body)
	if !strings.Contains(pageHTML, `name="csrf"`) {
		return "", nil, fmt.Errorf("OpenShift grant approval form not found at %s", redactOAuthURL(pageURL))
	}
	action := "approve"
	if m := reFormAction.FindStringSubmatch(pageHTML); len(m) >= 2 {
		action = m[1]
	}
	actionURL, err = resolveURL(pageURL, action)
	if err != nil {
		return "", nil, err
	}
	form = url.Values{}
	for _, m := range reHiddenInput.FindAllStringSubmatch(pageHTML, -1) {
		if len(m) >= 3 {
			form.Set(htmlpkg.UnescapeString(m[1]), htmlpkg.UnescapeString(m[2]))
		}
	}
	for _, m := range reCheckedScope.FindAllStringSubmatch(pageHTML, -1) {
		if len(m) >= 2 {
			form.Add("scope", htmlpkg.UnescapeString(m[1]))
		}
	}
	approve := "Allow selected permissions"
	if m := reApproveValue.FindStringSubmatch(pageHTML); len(m) >= 2 {
		approve = htmlpkg.UnescapeString(m[1])
	}
	form.Set("approve", approve)
	return actionURL, form, nil
}

func queryParam(rawURL, name string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(name)
}

func redactOAuthURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Has("code") {
		q.Set("code", "REDACTED")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func resolveURL(base, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("empty URL reference")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref, nil
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(ref, "//") {
		return baseURL.Scheme + ":" + ref, nil
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(refURL).String(), nil
}
