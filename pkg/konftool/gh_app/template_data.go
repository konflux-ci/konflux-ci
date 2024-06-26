package gh_app

import (
	"net/url"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/ghappregstate"
	"github.com/labstack/echo/v4"
)

// Data structure for passing data to the page template
type templateData struct {
	gitHubApp   *GitHubApp
	echoContext echo.Context
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
	// Apparently it takes a bit a crazyness to get a full URL
	urlStr := t.echoContext.Echo().Reverse(ghAppHomeRoute)
	if urlObj, err := url.Parse(urlStr); err == nil {
		if urlObj.Host == "" {
			if t.echoContext.Request().URL.Host == "" {
				urlObj.Host = t.echoContext.Request().Host
			} else {
				urlObj.Host = t.echoContext.Request().URL.Host
			}
			urlStr = urlObj.String()
		}
		if urlObj.Scheme == "" {
			if t.echoContext.Request().URL.Scheme == "" {
				urlObj.Scheme = "http"
			} else {
				urlObj.Scheme = t.echoContext.Request().URL.Scheme
			}
			urlStr = urlObj.String()
		}
	}
	return urlStr
}

func (t *templateData) GitHubNewAppURL() string {
	return "https://github.com/settings/apps/new"
}

func (t *templateData) AppRegState() ghappregstate.Enum {
	return t.gitHubApp.AppRegState
}
