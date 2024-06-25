package gh_app

import (
	"net/http"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/gh_app/templates"
	"github.com/konflux-ci/konflux-ci/pkg/konftool/webapps"
	"github.com/labstack/echo/v4"
)

type GitHubApp struct{}

func (ghapp *GitHubApp) SetupRoutes(e webapps.EchoLike) {
	e.GET("/", func(c echo.Context) error {
		//return c.String(http.StatusOK, "Hello from GitHub App controller")
		return c.Render(http.StatusOK, "gh_app.gohtml", nil)
	})
	e.GET("/another", func(c echo.Context) error {
		//return c.String(http.StatusOK, "Hello from GitHub App controller")
		return c.Render(http.StatusOK, "another.gohtml", nil)
	})
}

func (ghapp *GitHubApp) LoadTemplates(t webapps.TemplateLoader) error {
	return t.ParseFS(templates.All, "*.gohtml")
}
