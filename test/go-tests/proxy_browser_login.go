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
	"time"

	"github.com/konflux-ci/konflux-ci/operator/pkg/oauth2proxy"
)

const (
	// proxySessionCookieName is OAUTH2_PROXY_COOKIE_NAME from oauth2proxy.WithCookieConfig.
	proxySessionCookieName = oauth2proxy.SessionCookieName
	proxyLoginMaxHops      = 20

	// proxyLoginRetryTimeout is how long proxyCookieGETRetry waits for
	// a transient 502 or connection-refused error to clear. Dex restarts
	// when its projected ServiceAccount token is rotated; the new process
	// needs a few seconds to begin listening.
	proxyLoginRetryTimeout = 20 * time.Second

	// proxyLoginRetryInterval is the polling interval between retries.
	proxyLoginRetryInterval = 2 * time.Second
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

		status, location, body, err := proxyCookieGETRetry(ctx, client, current)
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

// proxyCookieGETRetry wraps proxyCookieGET with retries for transient errors
// that occur while Dex is restarting during a projected-token refresh. It
// retries HTTP 502 (Bad Gateway) and connection-refused/reset errors for up
// to proxyLoginRetryTimeout before returning the last result.
func proxyCookieGETRetry(ctx context.Context, client *http.Client, rawURL string) (int, string, []byte, error) {
	status, location, body, err := proxyCookieGET(ctx, client, rawURL)
	if !isRetriableLoginResponse(status, err) {
		return status, location, body, err
	}

	retryCtx, cancel := context.WithTimeout(ctx, proxyLoginRetryTimeout)
	defer cancel()

	for isRetriableLoginResponse(status, err) {
		select {
		case <-retryCtx.Done():
			// Deadline exceeded or parent context cancelled; return the
			// last attempt's result so the caller sees the real error.
			return status, location, body, err
		case <-time.After(proxyLoginRetryInterval):
		}
		fmt.Printf("[login-retry] %s: retrying after transient error (status=%d err=%v)\n", rawURL, status, err)
		status, location, body, err = proxyCookieGET(ctx, client, rawURL)
	}
	return status, location, body, err
}

// isRetriableLoginResponse reports whether the response from a login GET is
// a transient failure that may resolve when Dex finishes restarting.
func isRetriableLoginResponse(status int, err error) bool {
	if err != nil {
		return isTransientLoginError(err)
	}
	return status == http.StatusBadGateway
}

// isTransientLoginError reports whether err is a connection-refused or
// connection-reset dial error that may clear when Dex starts listening again.
func isTransientLoginError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "connection reset")
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
