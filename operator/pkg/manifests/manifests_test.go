package manifests

import (
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestAllComponents(t *testing.T) {
	components := AllComponents()
	if len(components) != 13 {
		t.Errorf("expected 13 components, got %d", len(components))
	}
}

func TestGetManifest(t *testing.T) {
	tests := []struct {
		component Component
		wantErr   bool
	}{
		{ApplicationAPI, false},
		{BuildService, false},
		{CertManager, false},
		{DefaultTenant, false},
		{EnterpriseContract, false},
		{ImageController, false},
		{Integration, false},
		{NamespaceLister, false},
		{RBAC, false},
		{Release, false},
		{UI, false},
		{Info, false},
		{Registry, false},
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

	if len(manifests) != 13 {
		t.Errorf("expected 13 manifests, got %d", len(manifests))
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

// testScheme returns a scheme that can decode core and CRD resources (used for GetCRDNamesForComponent).
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	return scheme
}

func TestGetCRDNamesForComponent(t *testing.T) {
	scheme := testScheme(t)
	store, err := NewObjectStore(scheme)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}

	tests := []struct {
		name      string
		component Component
		wantEmpty bool
		wantErr   bool
	}{
		{
			name:      "ApplicationAPI has CRDs",
			component: ApplicationAPI,
			wantEmpty: false,
			wantErr:   false,
		},
		{
			name:      "Registry has no CRDs",
			component: Registry,
			wantEmpty: true,
			wantErr:   false,
		},
		{
			name:      "unknown component returns error",
			component: Component("unknown"),
			wantEmpty: true,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names, err := store.GetCRDNamesForComponent(tt.component)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCRDNamesForComponent(%s) error = %v, wantErr %v", tt.component, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if tt.wantEmpty && len(names) != 0 {
				t.Errorf("GetCRDNamesForComponent(%s) expected empty names, got %v", tt.component, names)
			}
			if !tt.wantEmpty && len(names) == 0 {
				t.Errorf("GetCRDNamesForComponent(%s) expected non-empty names", tt.component)
			}
		})
	}
}
