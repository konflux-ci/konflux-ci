package go_tests

import (
	"context"
	"fmt"
	htmlpkg "html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"

	"github.com/konflux-ci/konflux-ci/operator/pkg/oauth2proxy"
)

const (
	// proxySessionCookieName is OAUTH2_PROXY_COOKIE_NAME from oauth2proxy.WithCookieConfig.
	proxySessionCookieName = oauth2proxy.SessionCookieName
	proxyLoginMaxHops      = 20
)

// obtainProxySessionCookie completes the browser login through /oauth2/start
// and returns the oauth2-proxy session cookies. On Kind that is the Dex static
// password form. On OpenShift it follows the same login and grant pages as the
// ID-token helper, then lets oauth2-proxy's callback set the cookie.
func obtainProxySessionCookie(ctx context.Context) ([]*http.Cookie, error) {
	user, pass, err := proxyLoginCredentials()
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout:   proxyHTTPRequestTimeout,
		Transport: proxyHTTPClient.Transport,
		Jar:       jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	current := proxyHome + "/oauth2/start?rd=" + url.QueryEscape("/api/k8s/api/v1/namespaces")
	for hop := 0; hop < proxyLoginMaxHops; hop++ {
		if cookies := proxySessionCookies(jar); len(cookies) > 0 {
			return cookies, nil
		}

		status, location, body, err := proxyCookieGET(ctx, client, current)
		if err != nil {
			return nil, err
		}
		if cookies := proxySessionCookies(jar); len(cookies) > 0 {
			return cookies, nil
		}

		if status >= 300 && status < 400 && location != "" {
			next, err := resolveURL(current, location)
			if err != nil {
				return nil, err
			}
			current = next
			continue
		}

		if status == http.StatusOK && isDexPasswordForm(body) {
			next, err := submitDexPasswordForm(ctx, client, current, body, user, pass)
			if err != nil {
				return nil, err
			}
			current = next
			continue
		}

		if status == http.StatusOK && strings.Contains(current, "/login") && strings.Contains(string(body), `name="csrf"`) {
			next, err := submitOpenShiftLoginForm(ctx, client, current, body, user, pass)
			if err != nil {
				return nil, err
			}
			current = next
			continue
		}

		if status == http.StatusOK && strings.Contains(current, "/authorize/approve") {
			next, err := submitOpenShiftGrantForm(ctx, client, current, body)
			if err != nil {
				return nil, err
			}
			current = next
			continue
		}

		return nil, fmt.Errorf("unexpected login response (http=%d, url=%s)", status, redactOAuthURL(current))
	}
	return nil, fmt.Errorf("session cookie %s was not set after %d hops", proxySessionCookieName, proxyLoginMaxHops)
}

// proxyLoginCredentials returns the Dex static user on Kind, or kubeadmin on OpenShift.
func proxyLoginCredentials() (string, string, error) {
	if !isProxyOpenShiftAuth() {
		return userName, password, nil
	}
	username := strings.TrimSpace(os.Getenv("OPENSHIFT_USERNAME"))
	if username == "" {
		username = "kubeadmin"
	}
	pass, err := (envOpenShiftPasswordSource{}).OpenShiftPassword()
	if err != nil {
		return "", "", err
	}
	return username, pass, nil
}

// proxySessionCookies returns oauth2-proxy session cookies stored for the UI host.
func proxySessionCookies(jar http.CookieJar) []*http.Cookie {
	home, err := url.Parse(proxyHome)
	if err != nil || jar == nil {
		return nil
	}
	var session []*http.Cookie
	for _, cookie := range jar.Cookies(home) {
		if isProxySessionCookie(cookie.Name) {
			session = append(session, cookie)
		}
	}
	return session
}

// isProxySessionCookie reports whether name is the session cookie or a numbered chunk of it.
func isProxySessionCookie(name string) bool {
	if name == proxySessionCookieName {
		return true
	}
	rest, ok := strings.CutPrefix(name, proxySessionCookieName+"_")
	if !ok || rest == "" || strings.HasPrefix(rest, "csrf") {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isDexPasswordForm reports whether body is Dex's static username and password form.
func isDexPasswordForm(body []byte) bool {
	page := string(body)
	return strings.Contains(page, `name="login"`) && strings.Contains(page, `name="password"`)
}

// submitDexPasswordForm posts the Dex login form and returns the redirect URL.
func submitDexPasswordForm(ctx context.Context, client *http.Client, pageURL string, body []byte, user, pass string) (string, error) {
	actionMatch := reFormAction.FindStringSubmatch(string(body))
	if len(actionMatch) < 2 {
		return "", fmt.Errorf("dex login form not found at %s", redactOAuthURL(pageURL))
	}
	actionURL, err := resolveURL(pageURL, htmlpkg.UnescapeString(actionMatch[1]))
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("login", user)
	form.Set("password", pass)
	return postFormForRedirect(ctx, client, actionURL, form)
}

// submitOpenShiftLoginForm posts the OpenShift login form and returns the redirect URL.
func submitOpenShiftLoginForm(ctx context.Context, client *http.Client, pageURL string, body []byte, user, pass string) (string, error) {
	actionURL, csrf, err := parseLoginForm(pageURL, body)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("csrf", csrf)
	form.Set("username", user)
	form.Set("password", pass)
	return postFormForRedirect(ctx, client, actionURL, form)
}

// submitOpenShiftGrantForm posts the OpenShift OAuth grant form and returns the redirect URL.
func submitOpenShiftGrantForm(ctx context.Context, client *http.Client, pageURL string, body []byte) (string, error) {
	actionURL, form, err := parseGrantForm(pageURL, body)
	if err != nil {
		return "", err
	}
	return postFormForRedirect(ctx, client, actionURL, form)
}

// postFormForRedirect posts form and returns the Location of the redirect response.
func postFormForRedirect(ctx context.Context, client *http.Client, actionURL string, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, actionURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, location, _, err := proxyCookieDo(client, req)
	if err != nil {
		return "", err
	}
	if status >= 300 && status < 400 && location != "" {
		return resolveURL(actionURL, location)
	}
	return "", fmt.Errorf("login form at %s returned HTTP %d without a redirect", redactOAuthURL(actionURL), status)
}

// proxyCookieGET GETs rawURL without following redirects. The client jar keeps cookies.
func proxyCookieGET(ctx context.Context, client *http.Client, rawURL string) (int, string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, "", nil, err
	}
	return proxyCookieDo(client, req)
}

// proxyCookieDo performs req and returns the status, Location, and body.
func proxyCookieDo(client *http.Client, req *http.Request) (int, string, []byte, error) {
	resp, err := client.Do(req)
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
