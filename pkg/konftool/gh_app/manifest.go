package gh_app

import (
	"encoding/json"

	"github.com/google/go-github/scrape"
	"github.com/google/go-github/v61/github"
)

func (t *templateData) Manifest() (string, error) {
	mjsb, err := json.Marshal(scrape.AppManifest{
		URL: github.String(t.KonfluxHomepageURL()),
		HookAttributes: map[string]string{"url": t.WebHookURL()},
		RedirectURL: github.String(t.RedirectURL()),
		//TODO: Add Description and let the user customize it
		Public:         github.Bool(t.AppIsPublic()),
		DefaultEvents: []string{
			"check_run",
			"check_suite",
			"issue_comment",
			"commit_comment",
			"pull_request",
			"push",
		},
		DefaultPermissions: &github.InstallationPermissions{
			Checks:           github.String("write"),
			Contents:         github.String("write"),
			Issues:           github.String("write"),
			Members:          github.String("read"),
			Metadata:         github.String("read"),
			OrganizationPlan: github.String("read"),
			PullRequests:     github.String("write"),
		},
	})
	return string(mjsb), err
}
