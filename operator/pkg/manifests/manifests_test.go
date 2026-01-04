package manifests

import (
	"strings"
	"testing"
)

func TestAllComponents(t *testing.T) {
	components := AllComponents()
	if len(components) != 11 {
		t.Errorf("expected 11 components, got %d", len(components))
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
		{Info, false},
		{CertManager, false},
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

	if len(manifests) != 11 {
		t.Errorf("expected 11 manifests, got %d", len(manifests))
	}

	for component, content := range manifests {
		if len(content) == 0 {
			t.Errorf("manifest for %s is empty", component)
		}
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
