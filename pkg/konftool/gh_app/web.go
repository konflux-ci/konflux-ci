package gh_app

import (
	"net/http"
	"net/url"
	"time"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/ghappregstate"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/templates"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/webapps"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/weberrors"
	"github.com/labstack/echo/v4"
)

const (
	ghAppHomeRoute = "gh_app_home"
	ghAppStoreKey  = "GitHub app"
	ghAppRegCodeParam = "code"
)

type GitHubApp struct {
	SetWebHookURL  *string
	AppRegState    ghappregstate.Enum
	AppRegCode     string
	AppRegCodeTime time.Time
	AppIsPublic    bool
}

func (ghapp *GitHubApp) SetupRoutes(s webapps.Store, e webapps.EchoLike) {
	var pageErrors weberrors.List
	_ = pageErrors.AddOnceIfNot(s.Get(ghAppStoreKey, ghapp), webapps.ErrKeyNotFound)

	e.GET("/", func(c echo.Context) error {
		// TODO: Temp hack to get a proper configuration for GitHub, Provide UI
		//       to edit this instead of hardcoding here
		tempWebHookURL := "https://smee.io/d3E32qmidjtLumte"
		ghapp.SetWebHookURL = &tempWebHookURL

		ghapp.checkAppRegCodeStillValid()
		if ghapp.hasAppRegCodeInQuery(c) {
			err := ghapp.storeAppRegCode(s, c.QueryParam(ghAppRegCodeParam))
			_ = pageErrors.AddOnce(err)
			return ghapp.redirectHome(c)
		}

		return c.Render(http.StatusOK, "gh_app.gohtml", &templateData{
			gitHubApp:   ghapp,
			pageErrors:  &pageErrors,
			echoContext: c,
		})
	}).Name = ghAppHomeRoute
}

func (ghapp *GitHubApp) LoadTemplates(t webapps.TemplateLoader) error {
	return t.ParseFS(templates.All, "*.gohtml")
}

func (ghapp *GitHubApp) checkAppRegCodeStillValid() {
	if ghapp.AppRegState == ghappregstate.FetchingAppData &&
		time.Since(ghapp.AppRegCodeTime) > time.Hour {
		ghapp.AppRegState = ghappregstate.Unconfigured
	}
}

func (ghapp *GitHubApp) hasAppRegCodeInQuery(c echo.Context) bool {
	// We ignore the app code in the HTTP query if we're not in the state of 
	// expecting it
	return ghapp.AppRegState == ghappregstate.Unconfigured && c.QueryParams().Has("code")
}

func (ghapp *GitHubApp) storeAppRegCode(s webapps.Store, code string) error {
	ghapp.AppRegState = ghappregstate.FetchingAppData
	ghapp.AppRegCode = code
	ghapp.AppRegCodeTime = time.Now()
	return s.Put(ghAppStoreKey, ghapp)
}

func (ghapp *GitHubApp) redirectHome(c echo.Context) error {
	return c.Redirect(http.StatusFound, ghapp.getHomeURL(c))
}

func (ghapp *GitHubApp) getHomeURL(c echo.Context) string {
	// Apparently it takes a bit a crazyness to get a full URL
	urlStr := c.Echo().Reverse(ghAppHomeRoute)
	if urlObj, err := url.Parse(urlStr); err == nil {
		if urlObj.Host == "" {
			if c.Request().URL.Host == "" {
				urlObj.Host = c.Request().Host
			} else {
				urlObj.Host = c.Request().URL.Host
			}
			urlStr = urlObj.String()
		}
		if urlObj.Scheme == "" {
			if c.Request().URL.Scheme == "" {
				urlObj.Scheme = "http"
			} else {
				urlObj.Scheme = c.Request().URL.Scheme
			}
			urlStr = urlObj.String()
		}
	}
	return urlStr
}
