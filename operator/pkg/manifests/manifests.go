// Package manifests provides embedded Kubernetes manifests from upstream kustomizations.
// The manifests are organized by component subdirectory, preserving the original structure.
package manifests

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
)

//go:embed all:application-api all:build-service all:enterprise-contract all:image-controller all:integration all:namespace-lister all:rbac all:release all:ui
var embeddedFS embed.FS

// Component represents a known upstream component.
type Component string

const (
	ApplicationAPI     Component = "application-api"
	BuildService       Component = "build-service"
	EnterpriseContract Component = "enterprise-contract"
	ImageController    Component = "image-controller"
	Integration        Component = "integration"
	NamespaceLister    Component = "namespace-lister"
	RBAC               Component = "rbac"
	Release            Component = "release"
	UI                 Component = "ui"
)

// AllComponents returns all available components.
func AllComponents() []Component {
	return []Component{
		ApplicationAPI,
		RBAC,
		EnterpriseContract,
		Release,
		BuildService,
		Integration,
		NamespaceLister,
		UI,
		ImageController,
	}
}

// GetManifest returns the manifest content for a specific component.
func GetManifest(component Component) ([]byte, error) {
	path := filepath.Join(string(component), "manifests.yaml")
	return embeddedFS.ReadFile(path)
}

// GetAllManifests returns a map of component names to their manifest content.
func GetAllManifests() (map[Component][]byte, error) {
	manifests := make(map[Component][]byte)
	for _, component := range AllComponents() {
		content, err := GetManifest(component)
		if err != nil {
			return nil, fmt.Errorf("failed to read manifest for %s: %w", component, err)
		}
		manifests[component] = content
	}
	return manifests, nil
}

// FS returns the embedded filesystem for direct access.
func FS() fs.FS {
	return embeddedFS
}

// ListComponents returns all component directories found in the embedded filesystem.
func ListComponents() ([]string, error) {
	entries, err := embeddedFS.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var components []string
	for _, entry := range entries {
		if entry.IsDir() {
			components = append(components, entry.Name())
		}
	}
	return components, nil
}

// ManifestInfo contains information about an embedded manifest.
type ManifestInfo struct {
	Component Component
	Path      string
	Content   []byte
}

// WalkManifests iterates over all embedded manifests and calls the provided function for each.
func WalkManifests(fn func(info ManifestInfo) error) error {
	for _, component := range AllComponents() {
		content, err := GetManifest(component)
		if err != nil {
			return err
		}
		info := ManifestInfo{
			Component: component,
			Path:      filepath.Join(string(component), "manifests.yaml"),
			Content:   content,
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}
