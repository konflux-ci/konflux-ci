package conformance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
)

func TestIsCollectDataGitHubAPIFlake(t *testing.T) {
	g := gomega.NewWithT(t)

	positive := "++ curl -s https://api.github.com/repos/konflux-ci/release-service-catalog/commits/c194bc7304852c66394290d2d1d36a7e848e3652\nparse error: Invalid numeric literal at line 1, column 10"
	g.Expect(isCollectDataGitHubAPIFlake(positive)).To(gomega.BeTrue(), "expected positive match for collect-data GitHub API flake logs")

	negative := []string{
		"",
		"api.github.com/repos/foo/bar/commits/deadbeef",
		"parse error: something else",
		"failed to push image to quay.io",
		"curl -s https://api.github.com/repos/foo/bar/commits/deadbeef\n" +
			strings.Repeat("x", collectDataGitHubAPIFlakeParseErrorWindow+1) + "\nparse error: unrelated jq failure",
	}
	for _, logs := range negative {
		g.Expect(isCollectDataGitHubAPIFlake(logs)).To(gomega.BeFalse(), "expected no match for logs: %q", logs)
	}
}

func TestParseNamespacedName(t *testing.T) {
	g := gomega.NewWithT(t)

	tests := []struct {
		input         string
		wantNamespace string
		wantName      string
		wantOK        bool
	}{
		{input: "default-managed-tenant/local-release", wantNamespace: "default-managed-tenant", wantName: "local-release", wantOK: true},
		{input: "  ns/obj  ", wantNamespace: "ns", wantName: "obj", wantOK: true},
		{input: "", wantOK: false},
		{input: "no-slash", wantOK: false},
		{input: "/missing-ns", wantOK: false},
		{input: "missing-name/", wantOK: false},
	}

	for _, tc := range tests {
		ns, name, ok := parseNamespacedName(tc.input)
		g.Expect(ok).To(gomega.Equal(tc.wantOK), "parseNamespacedName(%q) ok", tc.input)
		if !ok {
			continue
		}
		g.Expect(ns).To(gomega.Equal(tc.wantNamespace), "parseNamespacedName(%q) namespace", tc.input)
		g.Expect(name).To(gomega.Equal(tc.wantName), "parseNamespacedName(%q) name", tc.input)
	}
}

func TestParseGitHubOrgRepo(t *testing.T) {
	g := gomega.NewWithT(t)

	tests := []struct {
		url    string
		org    string
		repo   string
		wantOK bool
	}{
		{
			url:    "https://github.com/konflux-ci/release-service-catalog.git",
			org:    "konflux-ci",
			repo:   "release-service-catalog",
			wantOK: true,
		},
		{
			url:    "https://github.com/org/repo",
			org:    "org",
			repo:   "repo",
			wantOK: true,
		},
		{
			url:    "https://gitlab.com/org/repo.git",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		org, repo, ok := parseGitHubOrgRepo(tc.url)
		g.Expect(ok).To(gomega.Equal(tc.wantOK), "parseGitHubOrgRepo(%q) ok", tc.url)
		if !ok {
			continue
		}
		g.Expect(org).To(gomega.Equal(tc.org), "parseGitHubOrgRepo(%q) org", tc.url)
		g.Expect(repo).To(gomega.Equal(tc.repo), "parseGitHubOrgRepo(%q) repo", tc.url)
	}
}

func TestPipelineRefParam(t *testing.T) {
	g := gomega.NewWithT(t)

	ref := tektonutils.PipelineRef{
		Params: []tektonutils.Param{
			{Name: "url", Value: "https://github.com/konflux-ci/release-service-catalog.git"},
			{Name: "revision", Value: "abc123"},
		},
	}

	g.Expect(pipelineRefParam(ref, "revision")).To(gomega.Equal("abc123"))
	g.Expect(pipelineRefParam(ref, "missing")).To(gomega.BeEmpty())
}

func TestGitHubAPIProbedOnce(t *testing.T) {
	g := gomega.NewWithT(t)

	key := "default-managed-tenant/managed-test"
	t.Cleanup(func() { probedGitHubAPI.Delete(key) })

	g.Expect(githubAPIAlreadyProbed(key)).To(gomega.BeFalse(), "expected first probe attempt to run")
	g.Expect(githubAPIAlreadyProbed(key)).To(gomega.BeTrue(), "expected second probe attempt to be skipped")
}

func TestProbeGitHubCommitAPI(t *testing.T) {
	g := gomega.NewWithT(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Limit", "60")
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Resource", "core")
		got := r.Header.Get("Authorization")
		g.Expect(got).To(gomega.Or(gomega.BeEmpty(), gomega.Equal("Bearer test-token")), "unexpected Authorization header %q", got)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer server.Close()

	probeGitHubCommitAPI(server.URL, "test", "")
	probeGitHubCommitAPI(server.URL, "test-authenticated", "test-token")
}
