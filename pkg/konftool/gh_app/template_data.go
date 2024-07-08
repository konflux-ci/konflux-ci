package gh_app

import (
	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/ghappregstate"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/weberrors"
	"github.com/labstack/echo/v4"
)

// Data structure for passing data to the page template
type templateData struct {
	gitHubApp   *GitHubApp
	pageErrors  *weberrors.List
	echoContext echo.Context
}

func (t *templateData) Errors() weberrors.List {
	return *t.pageErrors
}

func (t *templateData) KonfluxHomepageURL() string {
	// TODO: Detect this value from the cluster or let the user customize it
	return "http://localhost:9443"
}

func (t *templateData) WebHookURL() string {
	return t.gitHubApp.webHookURL()
}

func (t *templateData) RejectedWebHookURL() bool {
	return t.gitHubApp.rejectedWebHookURL()
}

func (t *templateData) AppIsPublic() bool {
	return t.gitHubApp.appIsPublic()
}

func (t *templateData) RedirectURL() string {
	return t.gitHubApp.getHomeURL(t.echoContext)
}

func (t *templateData) GitHubNewAppURL() string {
	return "https://github.com/settings/apps/new"
}

func (t *templateData) AppRegState() ghappregstate.Enum {
	return t.gitHubApp.AppRegState
}
