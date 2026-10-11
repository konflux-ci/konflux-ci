package go_tests

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveURL(t *testing.T) {
	t.Parallel()
	base := "https://ui.example.com/oauth/authorize?foo=1"

	tests := []struct {
		ref  string
		want string
	}{
		{"https://other.example/path", "https://other.example/path"},
		{"//other.example/path", "https://other.example/path"},
		{"/login", "https://ui.example.com/login"},
		{"../authorize", "https://ui.example.com/authorize"},
		{"approve", "https://ui.example.com/oauth/approve"},
	}
	for _, tc := range tests {
		got, err := resolveURL(base, tc.ref)
		if err != nil {
			t.Fatalf("resolveURL(%q): %v", tc.ref, err)
		}
		if got != tc.want {
			t.Errorf("resolveURL(%q) = %q, want %q", tc.ref, got, tc.want)
		}
	}
}

func TestQueryParam(t *testing.T) {
	t.Parallel()
	raw := "https://ui.example.com/oauth2/callback?code=abc123&state=xyz"
	if got := queryParam(raw, "code"); got != "abc123" {
		t.Fatalf("queryParam code = %q", got)
	}
}

func TestRedactOAuthURL(t *testing.T) {
	t.Parallel()
	raw := "https://ui.example.com/oauth2/callback?code=secret&state=ok"
	got := redactOAuthURL(raw)
	if strings.Contains(got, "secret") {
		t.Fatalf("code not redacted: %s", got)
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("expected REDACTED in %s", got)
	}
}

func TestParseLoginForm(t *testing.T) {
	t.Parallel()
	body := []byte(`<html><form action="/login" method="post">
	<input name="csrf" type="hidden" value="tok123">
	</form></html>`)
	action, csrf, err := parseLoginForm("https://oauth.example.com/login?then=%2Fauthorize", body)
	if err != nil {
		t.Fatal(err)
	}
	if csrf != "tok123" {
		t.Fatalf("csrf = %q", csrf)
	}
	if action != "https://oauth.example.com/login?then=%2Fauthorize" {
		t.Fatalf("action = %q", action)
	}
}

func TestSelectIdentityProviderURL(t *testing.T) {
	t.Parallel()
	pageURL := "https://oauth.example.com/login"
	chooser := []byte(`
		<a href="/oauth/authorize?client_id=console&amp;idp=kube%3Aadmin">kube:admin</a>
		<a href="/oauth/authorize?client_id=console&idp=e2e-htpasswd">e2e-htpasswd</a>
		<a href="/oauth/authorize?client_id=console&idp=e2e-htpasswd">duplicate</a>
	`)

	t.Run("prefers OPENSHIFT_IDP match", func(t *testing.T) {
		got, ok := selectIdentityProviderURL(pageURL, chooser, "e2e-htpasswd")
		if !ok {
			t.Fatal("expected a match")
		}
		if want := "https://oauth.example.com/oauth/authorize?client_id=console&idp=e2e-htpasswd"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("preferred missing returns false", func(t *testing.T) {
		if _, ok := selectIdentityProviderURL(pageURL, chooser, "missing-idp"); ok {
			t.Fatal("expected no match for missing preferred IDP")
		}
	})

	t.Run("defaults to kube:admin when listed (kubeadmin credentials)", func(t *testing.T) {
		// Default OPENSHIFT_USERNAME is kubeadmin; do not post that password to
		// the sole non-kube provider when kube:admin is also on the chooser.
		got, ok := selectIdentityProviderURL(pageURL, chooser, "")
		if !ok {
			t.Fatal("expected a match")
		}
		if want := "https://oauth.example.com/oauth/authorize?client_id=console&idp=kube%3Aadmin"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("auto-selects sole non-kube:admin provider when kube:admin is absent", func(t *testing.T) {
		body := []byte(`<a href="/oauth/authorize?client_id=console&idp=e2e-htpasswd">e2e-htpasswd</a>`)
		got, ok := selectIdentityProviderURL(pageURL, body, "")
		if !ok {
			t.Fatal("expected a match")
		}
		if want := "https://oauth.example.com/oauth/authorize?client_id=console&idp=e2e-htpasswd"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("ambiguous non-kube providers without kube:admin return false", func(t *testing.T) {
		body := []byte(`
			<a href="/oauth/authorize?idp=htpasswd">htpasswd</a>
			<a href="/oauth/authorize?idp=ldap">ldap</a>
		`)
		if _, ok := selectIdentityProviderURL(pageURL, body, ""); ok {
			t.Fatal("expected no auto-select when multiple non-kube providers exist")
		}
	})

	t.Run("kube:admin plus multiple non-kube still selects kube:admin", func(t *testing.T) {
		body := []byte(`
			<a href="/oauth/authorize?idp=kube%3Aadmin">kube:admin</a>
			<a href="/oauth/authorize?idp=htpasswd">htpasswd</a>
			<a href="/oauth/authorize?idp=ldap">ldap</a>
		`)
		got, ok := selectIdentityProviderURL(pageURL, body, "")
		if !ok {
			t.Fatal("expected a match")
		}
		if !strings.Contains(got, "idp=kube%3Aadmin") && !strings.Contains(got, "idp=kube:admin") {
			t.Fatalf("expected kube:admin link, got %q", got)
		}
	})

	t.Run("empty chooser returns false", func(t *testing.T) {
		if _, ok := selectIdentityProviderURL(pageURL, []byte(`<html>no links</html>`), ""); ok {
			t.Fatal("expected no match")
		}
	})
}

func TestParseLoginFormIDPSpecificAction(t *testing.T) {
	t.Parallel()
	body := []byte(`<html><form action="/login/htpasswd" method="post">
	<input name="csrf" type="hidden" value="tok456">
	</form></html>`)
	action, csrf, err := parseLoginForm("https://oauth.example.com/login?then=%2Foauth%2Fauthorize", body)
	if err != nil {
		t.Fatal(err)
	}
	if csrf != "tok456" {
		t.Fatalf("csrf = %q", csrf)
	}
	if action != "https://oauth.example.com/login/htpasswd?then=%2Foauth%2Fauthorize" {
		t.Fatalf("action = %q", action)
	}
}

func TestParseGrantForm(t *testing.T) {
	t.Parallel()
	body := []byte(`<form action="approve">
	<input type="hidden" name="csrf" value="c&amp;1">
	<input type="hidden" name="client_id" value="oauth2-proxy">
	<input type="checkbox" checked name="scope" value="openid">
	<input type="checkbox" checked name="scope" value="groups">
	<input type="submit" name="approve" value="Allow selected permissions">
	</form>`)
	action, form, err := parseGrantForm("https://oauth.example.com/authorize/approve", body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(action, "/approve") {
		t.Fatalf("action = %q", action)
	}
	if form.Get("csrf") != "c&1" {
		t.Fatalf("csrf = %q", form.Get("csrf"))
	}
	if form["scope"] == nil || len(form["scope"]) != 2 {
		t.Fatalf("scope = %v", form["scope"])
	}
	if form.Get("approve") != "Allow selected permissions" {
		t.Fatalf("approve = %q", form.Get("approve"))
	}
}

func TestEnvOpenShiftPasswordSource(t *testing.T) {
	t.Setenv("OPENSHIFT_PASSWORD", "")
	t.Setenv("KUBEADMIN_PASSWORD_FILE", "")
	t.Setenv("SHARED_DIR", "")

	if _, err := (envOpenShiftPasswordSource{}).OpenShiftPassword(); err == nil {
		t.Fatal("expected error when no password configured")
	}

	t.Setenv("OPENSHIFT_PASSWORD", "from-env")
	got, err := (envOpenShiftPasswordSource{}).OpenShiftPassword()
	if err != nil || got != "from-env" {
		t.Fatalf("got %q err %v", got, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "kubeadmin-password")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENSHIFT_PASSWORD", "")
	t.Setenv("KUBEADMIN_PASSWORD_FILE", path)
	got, err = (envOpenShiftPasswordSource{}).OpenShiftPassword()
	if err != nil || got != "from-file" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestOpenShiftOAuthClient_ObtainIDToken(t *testing.T) {
	const clientSecret = "test-client-secret"
	var uiURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/idp/auth/openshift", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, uiURL+"/login", http.StatusFound)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<form action="/login"><input name="csrf" value="csrf-login"></form>`)
			return
		}
		http.Redirect(w, r, uiURL+"/authorize/approve", http.StatusFound)
	})
	mux.HandleFunc("/authorize/approve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<form action="approve">
				<input type="hidden" name="csrf" value="csrf-grant">
				<input type="checkbox" checked name="scope" value="openid">
				<input type="submit" name="approve" value="Allow selected permissions">
			</form>`)
			return
		}
		http.Redirect(w, r, uiURL+"/oauth2/callback?code=auth-code-123", http.StatusFound)
	})
	mux.HandleFunc("/oauth2/callback", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/idp/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != "auth-code-123" {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id_token":"eyJ.test.token"}`)
	})

	server := httptest.NewTLSServer(mux)
	defer server.Close()
	uiURL = strings.TrimRight(server.URL, "/")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport:     server.Client().Transport,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	oauth := &OpenShiftOAuthClient{http: client}
	token, err := oauth.ObtainIDToken(context.Background(), OpenShiftOAuthConfig{
		UIURL:        uiURL,
		Username:     "kubeadmin",
		Password:     "pass",
		ClientSecret: clientSecret,
	})
	if err != nil {
		t.Fatalf("ObtainIDToken: %v", err)
	}
	if token != "eyJ.test.token" {
		t.Fatalf("token = %q", token)
	}
}

func TestOpenShiftOAuthClient_ObtainIDTokenAliasCallback(t *testing.T) {
	const clientSecret = "test-client-secret"
	var issuerURL, aliasURL string
	var sawRedirectURI string

	issuerMux := http.NewServeMux()
	issuerMux.HandleFunc("/idp/auth/openshift", func(w http.ResponseWriter, r *http.Request) {
		sawRedirectURI = r.URL.Query().Get("redirect_uri")
		http.Redirect(w, r, issuerURL+"/login", http.StatusFound)
	})
	issuerMux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<form action="/login"><input name="csrf" value="csrf-login"></form>`)
			return
		}
		http.Redirect(w, r, aliasURL+"/oauth2/callback?code=auth-code-alias", http.StatusFound)
	})
	issuerMux.HandleFunc("/idp/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("redirect_uri") != aliasURL+"/oauth2/callback" {
			http.Error(w, "bad redirect_uri", http.StatusBadRequest)
			return
		}
		if r.Form.Get("code") != "auth-code-alias" {
			http.Error(w, "bad code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id_token":"eyJ.alias.token"}`)
	})

	issuerServer := httptest.NewTLSServer(issuerMux)
	defer issuerServer.Close()
	issuerURL = strings.TrimRight(issuerServer.URL, "/")

	aliasMux := http.NewServeMux()
	aliasMux.HandleFunc("/oauth2/callback", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	aliasServer := httptest.NewTLSServer(aliasMux)
	defer aliasServer.Close()
	aliasURL = strings.TrimRight(aliasServer.URL, "/")

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport:     issuerServer.Client().Transport,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	oauth := &OpenShiftOAuthClient{http: client}
	token, err := oauth.ObtainIDToken(context.Background(), OpenShiftOAuthConfig{
		UIURL:        aliasURL,
		IssuerURL:    issuerURL,
		Username:     "kubeadmin",
		Password:     "pass",
		ClientSecret: clientSecret,
	})
	if err != nil {
		t.Fatalf("ObtainIDToken: %v", err)
	}
	if token != "eyJ.alias.token" {
		t.Fatalf("token = %q", token)
	}
	if sawRedirectURI != aliasURL+"/oauth2/callback" {
		t.Fatalf("redirect_uri = %q, want alias callback", sawRedirectURI)
	}
}

func TestOpenShiftOAuthClient_ExchangeAuthorizationCodeError(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"error":"invalid_grant","error_description":"expired"}`)
	}))
	defer server.Close()

	uiURL := strings.TrimRight(server.URL, "/")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Transport:     server.Client().Transport,
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	oauth := &OpenShiftOAuthClient{http: client}
	_, err := oauth.exchangeAuthorizationCode(context.Background(), OpenShiftOAuthConfig{
		UIURL:        uiURL,
		ClientSecret: "secret",
	}, uiURL+"/oauth2/callback", "code")
	if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("expected invalid_grant error, got %v", err)
	}
}

func TestOpenShiftOAuthClient_ExchangeAuthorizationCodeInvalidJSON(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<html>not json</html>`)
	}))
	defer server.Close()

	uiURL := strings.TrimRight(server.URL, "/")
	oauth := &OpenShiftOAuthClient{http: server.Client()}
	_, err := oauth.exchangeAuthorizationCode(context.Background(), OpenShiftOAuthConfig{
		UIURL:        uiURL,
		ClientSecret: "secret",
	}, uiURL+"/oauth2/callback", "code")
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("expected decode response error, got %v", err)
	}
}

func TestProxyIDTokenForUser_OpenShiftMemory(t *testing.T) {
	t.Setenv(envProxyAuth, proxyAuthOpenShift)
	proxyOpenShiftIDToken = "memory-token"
	defer func() { proxyOpenShiftIDToken = "" }()

	token, ok := proxyIDTokenForUser("user2@konflux.dev")
	if !ok || token != "memory-token" {
		t.Fatalf("got %q ok=%v", token, ok)
	}
}
