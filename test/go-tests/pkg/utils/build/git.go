package build

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/core/v1"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/framework"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
)

// resolve the git url and revision from a pull request. If not found, return a default
// that is set from environment variables.
func ResolveGitDetails(repoUrlENV, repoRevisionENV string) (string, string, error) {
	defaultGitURL := fmt.Sprintf("https://github.com/%s/%s", constants.DEFAULT_GITHUB_BUILD_ORG, constants.DEFAULT_GITHUB_BUILD_REPO)
	defaultGitRevision := "main"
	// If we are testing the changes from a pull request, APP_SUFFIX may contain the
	// pull request ID. If it looks like an ID, then fetch information about the pull
	// request and use it to determine which git URL and revision to use for the EC
	// pipelines. NOTE: This is a workaround until Pipeline as Code supports passing
	// the source repo URL: https://issues.redhat.com/browse/SRVKP-3427. Once that's
	// implemented, remove the APP_SUFFIX support below and simply rely on the other
	// environment variables to set the git revision and URL directly.
	appSuffix := os.Getenv("APP_SUFFIX")
	if pullRequestID, err := strconv.ParseInt(appSuffix, 10, 64); err == nil {
		gh, err := github.NewClient(utils.GetEnv(constants.GITHUB_TOKEN_ENV, ""), constants.DEFAULT_GITHUB_BUILD_ORG)
		if err != nil {
			return "", "", err
		}
		return gh.GetPRDetails(constants.DEFAULT_GITHUB_BUILD_REPO, int(pullRequestID))

	}
	return utils.GetEnv(repoUrlENV, defaultGitURL), utils.GetEnv(repoRevisionENV, defaultGitRevision), nil
}

// CreateGitlabBuildSecret creates a Kubernetes secret for GitLab build credentials
func CreateGitlabBuildSecret(f *framework.Framework, secretName string, annotations map[string]string, token string) error {
	buildSecret := v1.Secret{}
	buildSecret.Name = secretName
	buildSecret.Labels = map[string]string{
		"appstudio.redhat.com/credentials": "scm",
		"appstudio.redhat.com/scm.host":    "gitlab.com",
	}
	if annotations != nil {
		buildSecret.Annotations = annotations
	}
	buildSecret.Type = "kubernetes.io/basic-auth"
	buildSecret.StringData = map[string]string{
		"password": token,
	}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(f.UserNamespace, &buildSecret)
	if err != nil {
		return fmt.Errorf("error creating build secret: %v", err)
	}
	return nil
}

// CreateCodebergBuildSecret creates a Kubernetes secret for Codeberg/Forgejo build credentials
func CreateCodebergBuildSecret(f *framework.Framework, secretName string, annotations map[string]string, token string) error {
	buildSecret := v1.Secret{}
	buildSecret.Name = secretName
	buildSecret.Labels = map[string]string{
		"appstudio.redhat.com/credentials": "scm",
		"appstudio.redhat.com/scm.host":    "codeberg.org",
	}
	if annotations != nil {
		buildSecret.Annotations = annotations
	}
	buildSecret.Type = "kubernetes.io/basic-auth"
	buildSecret.StringData = map[string]string{
		"password": token,
	}
	_, err := f.AsKubeAdmin.CommonController.CreateSecret(f.UserNamespace, &buildSecret)
	if err != nil {
		return fmt.Errorf("error creating build secret: %v", err)
	}
	return nil
}

func CleanupWebhooks(f *framework.Framework, repoName string) error {
	hooks, err := f.AsKubeAdmin.CommonController.GitHub.ListRepoWebhooks(repoName)
	if err != nil {
		return err
	}
	for _, h := range hooks {
		hookUrl := h.Config["url"].(string)
		if strings.Contains(hookUrl, f.ClusterAppDomain) {
			ginkgo.GinkgoWriter.Printf("removing webhook URL: %s\n", hookUrl)
			err = f.AsKubeAdmin.CommonController.GitHub.DeleteWebhook(repoName, h.GetID())
			if err != nil {
				return err
			}
			break
		}
	}
	return nil
}
