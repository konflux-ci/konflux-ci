package gh_app

import (
	"net/http"

	"github.com/konflux-ci/konflux-ci/pkg/konftool/webapps"
	"github.com/labstack/echo/v4"
)

type GitHubApp struct{}

func (ghapp *GitHubApp) SetupRoutes(e webapps.EchoLike) {
	e.GET("/", func(c echo.Context) error {
		return c.String(http.StatusOK, "Hello from GitHub App controller")
	})
}