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
	// UIURL is the public UI base used for oauth2-proxy's redirect_uri
	// (…/oauth2/callback). For additionalFQDNs this is the alias host.
	UIURL string
	// IssuerURL is the Dex issuer base used for /idp/auth and /idp/token.
	// When empty, UIURL is used (single-host installs). For additionalFQDNs
	// this must stay on the primary UI host — Dex rejects issuer requests on aliases.
	IssuerURL    string
	Username     string
	Password     string
	ClientSecret string
	Scope        string
	MaxRedirects int
}

func (cfg OpenShiftOAuthConfig) issuerBase() string {
	if s := strings.TrimRight(strings.TrimSpace(cfg.IssuerURL), "/"); s != "" {
		return s
	}
	return strings.TrimRight(strings.TrimSpace(cfg.UIURL), "/")
}

func (cfg OpenShiftOAuthConfig) uiBase() string {
	return strings.TrimRight(strings.TrimSpace(cfg.UIURL), "/")
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
	secret, err := (kubeOAuth2ProxySecretSource{client: kube, namespace: uiNamespaceForProxyTests()}).OAuth2ProxyClientSecret(ctx)
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

// obtainOpenShiftProxyIDTokenForUI obtains a Dex id_token for uiURL's oauth2-proxy
// callback while performing Dex authorization against issuerURL. Use this when
// uiURL is an additionalFQDN alias and issuerURL is the primary UI host.
func obtainOpenShiftProxyIDTokenForUI(ctx context.Context, baseHTTP *http.Client, kube crclient.Client, issuerURL, uiURL string) (string, error) {
	oauthHTTP, err := newOpenShiftOAuthClient(baseHTTP)
	if err != nil {
		return "", err
	}
	password, err := (envOpenShiftPasswordSource{}).OpenShiftPassword()
	if err != nil {
		return "", err
	}
	secret, err := (kubeOAuth2ProxySecretSource{client: kube, namespace: uiNamespaceForProxyTests()}).OAuth2ProxyClientSecret(ctx)
	if err != nil {
		return "", err
	}
	username := strings.TrimSpace(os.Getenv("OPENSHIFT_USERNAME"))
	if username == "" {
		username = "kubeadmin"
	}
	cfg := OpenShiftOAuthConfig{
		UIURL:        strings.TrimRight(strings.TrimSpace(uiURL), "/"),
		IssuerURL:    strings.TrimRight(strings.TrimSpace(issuerURL), "/"),
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
	redirectURI := cfg.uiBase() + "/oauth2/callback"
	authCode, err := c.obtainAuthorizationCode(ctx, cfg, redirectURI)
	if err != nil {
		return "", err
	}
	return c.exchangeAuthorizationCode(ctx, cfg, redirectURI, authCode)
}

func (c *OpenShiftOAuthClient) obtainAuthorizationCode(ctx context.Context, cfg OpenShiftOAuthConfig, redirectURI string) (string, error) {
	start, err := url.Parse(cfg.issuerBase() + "/idp/auth/openshift")
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

		// OpenShift shows an IDP chooser at /oauth/authorize when multiple providers exist.
		if status == http.StatusOK && strings.Contains(current, "/oauth/authorize") {
			if next, ok := selectIdentityProviderURL(current, body, preferredOpenShiftIDP()); ok {
				pendingAuthorize = next
				current = next
				continue
			}
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
		next, err := resolveURL(action, location)
		if err != nil {
			return "", err
		}
		// Some OpenShift login responses redirect to "/" instead of back to
		// /oauth/authorize; resume the pending authorize URL in that case.
		if pendingAuthorize != "" {
			if u, err := url.Parse(next); err == nil && (u.Path == "" || u.Path == "/") {
				return pendingAuthorize, nil
			}
		}
		return next, nil
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
	tokenURL := cfg.issuerBase() + "/idp/token"
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
	reIDPHref      = regexp.MustCompile(`(?i)href="([^"]*idp=[^"]*)"`)
)

// preferredOpenShiftIDP returns OPENSHIFT_IDP when set (e.g. e2e-htpasswd on
// clusters that show an identity-provider chooser alongside kube:admin).
func preferredOpenShiftIDP() string {
	return strings.TrimSpace(os.Getenv("OPENSHIFT_IDP"))
}

// selectIdentityProviderURL finds an /oauth/authorize?...&idp=... link on the
// OpenShift login chooser. Prefer matches preferred when set (OPENSHIFT_IDP).
// Otherwise prefer kube:admin when listed — obtainOpenShiftProxyIDToken defaults
// to kubeadmin credentials. If kube:admin is absent, fall back to a sole
// non-kube:admin provider (e.g. htpasswd-only choosers).
func selectIdentityProviderURL(pageURL string, body []byte, preferred string) (string, bool) {
	matches := reIDPHref.FindAllStringSubmatch(string(body), -1)
	if len(matches) == 0 {
		return "", false
	}
	type choice struct {
		name string
		href string
	}
	var choices []choice
	seen := map[string]struct{}{}
	for _, m := range matches {
		href := htmlpkg.UnescapeString(m[1])
		abs, err := resolveURL(pageURL, href)
		if err != nil {
			continue
		}
		idpName := queryParam(abs, "idp")
		if idpName == "" {
			continue
		}
		if _, ok := seen[idpName]; ok {
			continue
		}
		seen[idpName] = struct{}{}
		choices = append(choices, choice{name: idpName, href: abs})
	}
	if len(choices) == 0 {
		return "", false
	}
	if preferred != "" {
		for _, c := range choices {
			if c.name == preferred {
				return c.href, true
			}
		}
		return "", false
	}
	for _, c := range choices {
		if c.name == "kube:admin" {
			return c.href, true
		}
	}
	var nonKube []choice
	for _, c := range choices {
		if c.name != "kube:admin" {
			nonKube = append(nonKube, c)
		}
	}
	if len(nonKube) == 1 {
		return nonKube[0].href, true
	}
	return "", false
}

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
	// Preserve ?then=... from the login page. OpenShift IDP-specific forms use
	// action="/login/<idp>") without the query string; posting without then=
	// drops the user on "/" instead of returning to /oauth/authorize.
	// Keep the form action path (do not rewrite /login/<idp> back to /login).
	actionPath := action
	if u, err := url.Parse(actionURL); err == nil && u.Path != "" {
		actionPath = u.Path
	}
	if actionPath == "/login" || actionPath == "login" || strings.HasPrefix(actionPath, "/login/") {
		pageU, err := url.Parse(pageURL)
		if err != nil {
			return "", "", err
		}
		then := pageU.Query().Get("then")
		if then != "" {
			actionU, err := url.Parse(actionURL)
			if err != nil {
				return "", "", err
			}
			if actionU.Query().Get("then") == "" {
				q := actionU.Query()
				q.Set("then", then)
				actionU.RawQuery = q.Encode()
				actionURL = actionU.String()
			}
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
