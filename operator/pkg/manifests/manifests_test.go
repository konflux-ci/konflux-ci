package manifests

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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

func TestKonfluxCAAndIssuerLifetimes(t *testing.T) {
	const (
		caDuration      = "8760h"
		caRenewBefore   = "4380h"
		leafDuration    = "2160h"
		leafRenewBefore = "720h"
	)

	caDurationParsed, err := time.ParseDuration(caDuration)
	if err != nil {
		t.Fatalf("parse CA duration: %v", err)
	}
	caRenewBeforeParsed, err := time.ParseDuration(caRenewBefore)
	if err != nil {
		t.Fatalf("parse CA renewBefore: %v", err)
	}
	leafDurationParsed, err := time.ParseDuration(leafDuration)
	if err != nil {
		t.Fatalf("parse leaf duration: %v", err)
	}
	if leafDurationParsed >= caRenewBeforeParsed {
		t.Fatalf("leaf duration %s must be less than konflux-ca renewBefore %s", leafDuration, caRenewBefore)
	}
	if caRenewBeforeParsed >= caDurationParsed {
		t.Fatalf("konflux-ca renewBefore %s must be less than its duration %s", caRenewBefore, caDuration)
	}

	scheme := testScheme(t)
	store, err := NewObjectStore(scheme)
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}

	foundCA := false
	foundIssuerCert := false
	for _, component := range AllComponents() {
		objects, err := store.GetForComponent(component)
		if err != nil {
			t.Fatalf("GetForComponent(%s) error = %v", component, err)
		}
		for _, obj := range objects {
			if obj.GetObjectKind().GroupVersionKind().Kind != "Certificate" {
				continue
			}
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				t.Fatalf("%s/%s Certificate was not decoded as unstructured", obj.GetNamespace(), obj.GetName())
			}
			spec, found, err := unstructured.NestedMap(u.Object, "spec")
			if err != nil || !found {
				t.Fatalf("%s/%s missing spec: %v", u.GetNamespace(), u.GetName(), err)
			}
			issuerName, _, err := unstructured.NestedString(spec, "issuerRef", "name")
			if err != nil {
				t.Fatalf("%s/%s issuerRef.name: %v", u.GetNamespace(), u.GetName(), err)
			}

			if u.GetName() == "konflux-ca" && u.GetNamespace() == "cert-manager" {
				foundCA = true
				assertStringField(t, u, spec, "duration", caDuration)
				assertStringField(t, u, spec, "renewBefore", caRenewBefore)
				privateKey, found, err := unstructured.NestedMap(spec, "privateKey")
				if err != nil || !found {
					t.Fatalf("konflux-ca missing spec.privateKey: %v", err)
				}
				assertStringField(t, u, privateKey, "rotationPolicy", "Never")
			}

			if issuerName != "konflux-issuer" {
				continue
			}
			foundIssuerCert = true
			assertStringField(t, u, spec, "duration", leafDuration)
			assertStringField(t, u, spec, "renewBefore", leafRenewBefore)
		}
	}

	if !foundCA {
		t.Fatal("embedded manifests did not include cert-manager/konflux-ca")
	}
	if !foundIssuerCert {
		t.Fatal("embedded manifests did not include a certificate issued by konflux-issuer")
	}
}

func assertStringField(t *testing.T, obj *unstructured.Unstructured, fields map[string]any, key, want string) {
	t.Helper()
	got, ok := fields[key].(string)
	if !ok || got != want {
		t.Errorf("%s/%s spec.%s = %#v, want %q", obj.GetNamespace(), obj.GetName(), key, fields[key], want)
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

// TestDeploymentsSpreadAcrossZonesAndNodes guards the hardcoded soft spread of
// every scalable operator-managed Deployment.
func TestDeploymentsSpreadAcrossZonesAndNodes(t *testing.T) {
	want := map[string]bool{
		"proxy":                                  true,
		"dex":                                    true,
		"namespace-lister":                       true,
		"build-service-controller-manager":       true,
		"integration-service-controller-manager": true,
		"release-service-controller-manager":     true,
		"image-controller-controller-manager":    true,
	}

	store, err := NewObjectStore(testScheme(t))
	if err != nil {
		t.Fatalf("NewObjectStore() error = %v", err)
	}
	for _, component := range AllComponents() {
		objects, err := store.GetForComponent(component)
		if err != nil {
			t.Fatalf("GetForComponent(%s) error = %v", component, err)
		}
		for _, obj := range objects {
			d, ok := obj.(*appsv1.Deployment)
			if !ok || !want[d.Name] {
				continue
			}
			delete(want, d.Name)
			podLabels := d.Spec.Template.Labels
			got := map[string]bool{}
			for _, c := range d.Spec.Template.Spec.TopologySpreadConstraints {
				if c.MaxSkew != 1 || c.WhenUnsatisfiable != corev1.ScheduleAnyway {
					t.Errorf("%s: unexpected constraint %+v", d.Name, c)
				}
				if c.LabelSelector == nil || !labels.SelectorFromSet(c.LabelSelector.MatchLabels).Matches(labels.Set(podLabels)) {
					t.Errorf("%s: labelSelector %v does not match pod labels %v", d.Name, c.LabelSelector, podLabels)
				}
				got[c.TopologyKey] = true
			}
			for _, key := range []string{"topology.kubernetes.io/zone", "kubernetes.io/hostname"} {
				if !got[key] {
					t.Errorf("%s: missing topologySpreadConstraint for %s", d.Name, key)
				}
			}
		}
	}
	for name := range want {
		t.Errorf("deployment %s not found in embedded manifests", name)
	}
}
