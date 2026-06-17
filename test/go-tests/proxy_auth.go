package go_tests

import (
	"fmt"
	"os"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
)

const (
	envProxyAuth       = "KONFLUX_PROXY_AUTH"
	envProxyAuthMethod = "KONFLUX_PROXY_AUTH_METHOD"
	envProxyIDToken    = "KONFLUX_PROXY_ID_TOKEN"

	envProxyIDTokenUser1 = "KONFLUX_PROXY_ID_TOKEN_USER1"
	envProxyIDTokenUser2 = "KONFLUX_PROXY_ID_TOKEN_USER2"

	proxyAuthOpenShift = "openshift"
	proxyAuthDex       = "dex"

	proxyAuthMethodOpenShiftOAuth   = "openshift-oauth"
	proxyAuthMethodDexPasswordGrant = "dex-password-grant"
)

var logProxyAuthOnce sync.Once

var proxyOpenShiftIDToken string

func setProxyOpenShiftIDToken(token string) {
	proxyOpenShiftIDToken = token
}

func proxyAuthMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv(envProxyAuth)))
	if mode == "" {
		return proxyAuthDex
	}
	return mode
}

func proxyAuthMethodFromEnv() string {
	if method := strings.TrimSpace(os.Getenv(envProxyAuthMethod)); method != "" {
		return method
	}
	if isProxyOpenShiftAuth() {
		return proxyAuthMethodOpenShiftOAuth
	}
	return proxyAuthMethodDexPasswordGrant
}

func logProxyAuthMethod(method string) {
	logProxyAuthOnce.Do(func() {
		line := fmt.Sprintf("proxy auth method: %s\n", method)
		_, _ = fmt.Fprint(os.Stderr, line)
		_, _ = fmt.Fprint(GinkgoWriter, line)
	})
}

func proxyIDTokenFromEnv(user string) (string, bool) {
	switch strings.TrimSpace(user) {
	case "user1@konflux.dev":
		if token := strings.TrimSpace(os.Getenv(envProxyIDTokenUser1)); token != "" {
			return token, true
		}
	case "user2@konflux.dev":
		if token := strings.TrimSpace(os.Getenv(envProxyIDTokenUser2)); token != "" {
			return token, true
		}
	}

	if token := strings.TrimSpace(os.Getenv(envProxyIDToken)); token != "" {
		return token, true
	}
	return "", false
}

func proxyIDTokenForUser(user string) (string, bool) {
	if token, ok := proxyIDTokenFromEnv(user); ok {
		return token, true
	}
	if isProxyOpenShiftAuth() && proxyOpenShiftIDToken != "" {
		return proxyOpenShiftIDToken, true
	}
	return "", false
}

func isProxyOpenShiftAuth() bool {
	return proxyAuthMode() == proxyAuthOpenShift
}
