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
