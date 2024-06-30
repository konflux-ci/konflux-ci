package gh_app

import "net/url"

const (
	defaultWebHookURL = "http://localhost:8080"
)


func (g *GitHubApp) webHookURL() string {
	// TODO: be smarter about getting this e.g.: 
	// - find out from the cluster
	// - get Smee configuration
	if g.SetWebHookURL == nil {
		return defaultWebHookURL
	}
	return *g.SetWebHookURL
}

// Return whether the webhook URL would be rejectd by GitHub
func (g *GitHubApp)rejectedWebHookURL() bool {
	u, err := url.Parse(g.webHookURL())
	return err != nil || u.Hostname() == "localhost"
}

func (g *GitHubApp)appIsPublic() bool {
	return g.AppIsPublic
}
