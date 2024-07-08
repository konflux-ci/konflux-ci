package gh_app

import (
	"net/http"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/ghappregstate"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/templates"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/webapps"
	"github.com/labstack/echo/v4"
)

const (
	ghAppHomeRoute = "gh_app_home"
)

type GitHubApp struct {
	SetWebHookURL *string
	AppRegState ghappregstate.Enum
	AppRegCode string
	AppIsPublic bool
}

func (ghapp *GitHubApp) SetupRoutes(e webapps.EchoLike) {
	e.GET("/", func(c echo.Context) error {
		// TODO: Temp hack to get a proper configuration for GitHub, Provide UI
		//       to edit this instead of hardcoding here
		tempWebHookURL := "https://smee.io/d3E32qmidjtLumte"
		ghapp.SetWebHookURL = &tempWebHookURL

		if c.QueryParams().Has("code") {
			ghapp.AppRegState = ghappregstate.FetchingAppData
			ghapp.AppRegCode = c.QueryParam("code")
		}
		return c.Render(http.StatusOK, "gh_app.gohtml", &templateData{
			gitHubApp:   ghapp,
			echoContext: c,
		})
	}).Name = ghAppHomeRoute
}

func (ghapp *GitHubApp) LoadTemplates(t webapps.TemplateLoader) error {
	return t.ParseFS(templates.All, "*.gohtml")
}
