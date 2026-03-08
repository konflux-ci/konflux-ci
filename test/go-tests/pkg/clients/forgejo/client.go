package forgejo

import (
	"codeberg.org/mvdkleijn/forgejo-sdk/forgejo/v2"
)

// ForgejoClient wraps the Forgejo SDK client
type ForgejoClient struct {
	client *forgejo.Client
	org    string
}

// NewForgejoClient creates a new Forgejo client
func NewForgejoClient(accessToken, baseURL, org string) (*ForgejoClient, error) {
	client, err := forgejo.NewClient(baseURL, forgejo.SetToken(accessToken))
	if err != nil {
		return nil, err
	}

	return &ForgejoClient{
		client: client,
		org:    org,
	}, nil
}

// GetClient returns the underlying Forgejo client
func (fc *ForgejoClient) GetClient() *forgejo.Client {
	return fc.client
}

// GetOrg returns the organization name
func (fc *ForgejoClient) GetOrg() string {
	return fc.org
}
