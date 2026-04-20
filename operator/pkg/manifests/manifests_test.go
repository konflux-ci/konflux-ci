package manifests

import (
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

func TestAllComponents(t *testing.T) {
	components := AllComponents()
	if len(components) != 15 {
		t.Errorf("expected 15 components, got %d", len(components))
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
		{CLI, false},
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
		{SegmentBridge, false},
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

	if len(manifests) != 15 {
		t.Errorf("expected 15 manifests, got %d", len(manifests))
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

// TestParseManifests directly tests the unexported parseManifests function.
// The test file is in package manifests so parseManifests is directly callable.
func TestParseManifests(t *testing.T) {
	// Build a minimal scheme that knows about core Kubernetes types (e.g. Deployment).
	// Unknown types (CRDs, Tekton resources) fall back to unstructured.Unstructured.
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	deploymentYAML := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test
        image: nginx:latest
`

	unknownYAML := `apiVersion: example.io/v1alpha1
kind: MyCustomResource
metadata:
  name: my-resource
  namespace: default
`

	tests := []struct {
		name      string
		input     []byte
		wantCount int
		wantErr   bool
	}{
		{
			name:      "single typed object decoded as *appsv1.Deployment",
			input:     []byte(deploymentYAML),
			wantCount: 1,
			wantErr:   false,
		},
		{
			name: "multi-document YAML returns two objects in order",
			input: []byte(deploymentYAML + "---\n" + deploymentYAML),
			wantCount: 2,
			wantErr:   false,
		},
		{
			name: "empty documents between separators are skipped",
			input: []byte("---\n" + deploymentYAML + "---\n\n---\n" + deploymentYAML),
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "unknown apiVersion falls back to unstructured.Unstructured",
			input:     []byte(unknownYAML),
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:      "malformed YAML returns an error",
			input:     []byte("this: is: not: valid: yaml: [{"),
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:      "empty input returns empty slice with no error",
			input:     []byte{},
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects, err := parseManifests(decoder, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseManifests() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(objects) != tt.wantCount {
				t.Errorf("parseManifests() returned %d objects, want %d", len(objects), tt.wantCount)
				return
			}
		})
	}
}

// TestParseManifestsTypedDecoding verifies that a registered type is decoded
// into its concrete Go type (not unstructured).
func TestParseManifestsTypedDecoding(t *testing.T) {
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()

	yaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: typed-deployment
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: typed
  template:
    metadata:
      labels:
        app: typed
    spec:
      containers:
      - name: app
        image: nginx:latest
`
	objects, err := parseManifests(decoder, []byte(yaml))
	if err != nil {
		t.Fatalf("parseManifests() unexpected error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("parseManifests() returned %d objects, want 1", len(objects))
	}
	if _, ok := objects[0].(*appsv1.Deployment); !ok {
		t.Errorf("parseManifests() returned %T, want *appsv1.Deployment", objects[0])
	}
}
