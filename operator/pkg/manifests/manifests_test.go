package manifests

import (
	"strings"
	"testing"
)

func TestAllComponents(t *testing.T) {
	components := AllComponents()
	if len(components) != 9 {
		t.Errorf("expected 9 components, got %d", len(components))
	}
}

func TestGetManifest(t *testing.T) {
	tests := []struct {
		component Component
		wantErr   bool
	}{
		{ApplicationAPI, false},
		{BuildService, false},
		{EnterpriseContract, false},
		{ImageController, false},
		{Integration, false},
		{NamespaceLister, false},
		{RBAC, false},
		{Release, false},
		{UI, false},
		{Component("nonexistent"), true},
	}

	for _, tt := range tests {
		t.Run(string(tt.component), func(t *testing.T) {
			content, err := GetManifest(tt.component)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetManifest(%s) error = %v, wantErr %v", tt.component, err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(content) == 0 {
				t.Errorf("GetManifest(%s) returned empty content", tt.component)
			}
		})
	}
}

func TestGetAllManifests(t *testing.T) {
	manifests, err := GetAllManifests()
	if err != nil {
		t.Fatalf("GetAllManifests() error = %v", err)
	}

	if len(manifests) != 9 {
		t.Errorf("expected 9 manifests, got %d", len(manifests))
	}

	for component, content := range manifests {
		if len(content) == 0 {
			t.Errorf("manifest for %s is empty", component)
		}
	}
}

func TestListComponents(t *testing.T) {
	components, err := ListComponents()
	if err != nil {
		t.Fatalf("ListComponents() error = %v", err)
	}

	if len(components) != 9 {
		t.Errorf("expected 9 components, got %d", len(components))
	}

	expectedComponents := map[string]bool{
		"application-api":     true,
		"build-service":       true,
		"enterprise-contract": true,
		"image-controller":    true,
		"integration":         true,
		"namespace-lister":    true,
		"rbac":                true,
		"release":             true,
		"ui":                  true,
	}

	for _, c := range components {
		if !expectedComponents[c] {
			t.Errorf("unexpected component: %s", c)
		}
	}
}

func TestFS(t *testing.T) {
	fsys := FS()
	if fsys == nil {
		t.Fatal("FS() returned nil")
	}
}

func TestWalkManifests(t *testing.T) {
	var count int
	err := WalkManifests(func(info ManifestInfo) error {
		count++
		if info.Component == "" {
			t.Error("ManifestInfo.Component is empty")
		}
		if info.Path == "" {
			t.Error("ManifestInfo.Path is empty")
		}
		if len(info.Content) == 0 {
			t.Errorf("ManifestInfo.Content is empty for %s", info.Component)
		}
		if !strings.HasSuffix(info.Path, "manifests.yaml") {
			t.Errorf("ManifestInfo.Path should end with manifests.yaml, got %s", info.Path)
		}
		return nil
	})

	if err != nil {
		t.Fatalf("WalkManifests() error = %v", err)
	}

	if count != 9 {
		t.Errorf("expected 9 manifests, walked %d", count)
	}
}

func TestManifestContent(t *testing.T) {
	// Verify that manifests contain valid YAML (basic check for apiVersion)
	content, err := GetManifest(ApplicationAPI)
	if err != nil {
		t.Fatalf("GetManifest(ApplicationAPI) error = %v", err)
	}

	if !strings.Contains(string(content), "apiVersion:") {
		t.Error("application-api manifest doesn't contain 'apiVersion:'")
	}
}
