// Package manifests provides embedded Kubernetes manifests from upstream kustomizations.
// The manifests are organized by component subdirectory, preserving the original structure.
package manifests

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"io"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

//go:embed all:application-api all:build-service all:enterprise-contract all:image-controller all:integration all:namespace-lister all:rbac all:release all:ui all:info all:cert-manager all:registry all:default-tenant
var embeddedFS embed.FS

// Component represents a known upstream component.
type Component string

const (
	ApplicationAPI     Component = "application-api"
	BuildService       Component = "build-service"
	CertManager        Component = "cert-manager"
	DefaultTenant      Component = "default-tenant"
	EnterpriseContract Component = "enterprise-contract"
	ImageController    Component = "image-controller"
	Integration        Component = "integration"
	NamespaceLister    Component = "namespace-lister"
	RBAC               Component = "rbac"
	Registry           Component = "registry"
	Release            Component = "release"
	UI                 Component = "ui"
	Info               Component = "info"
)

// AllComponents returns all available components.
func AllComponents() []Component {
	return []Component{
		ApplicationAPI,
		RBAC,
		CertManager,
		DefaultTenant,
		EnterpriseContract,
		Release,
		BuildService,
		Integration,
		NamespaceLister,
		Registry,
		UI,
		ImageController,
		Info,
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

// ObjectStore holds parsed Kubernetes objects organized by component.
// It provides methods for retrieving deep copies of objects to prevent
// mutation of the stored objects during reconciliation.
type ObjectStore struct {
	objects map[Component][]client.Object
}

// NewObjectStore parses all embedded manifests using the provided scheme and
// returns an ObjectStore containing the parsed objects.
// Types registered in the scheme are decoded into typed objects (e.g., *appsv1.Deployment).
// Types not registered in the scheme are decoded as *unstructured.Unstructured.
func NewObjectStore(scheme *runtime.Scheme) (*ObjectStore, error) {
	decoder := serializer.NewCodecFactory(scheme).UniversalDeserializer()
	objects := make(map[Component][]client.Object)

	for _, component := range AllComponents() {
		content, err := GetManifest(component)
		if err != nil {
			return nil, fmt.Errorf("failed to read manifest for %s: %w", component, err)
		}
		parsed, err := parseManifests(decoder, content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse manifest for %s: %w", component, err)
		}
		objects[component] = parsed
	}

	return &ObjectStore{objects: objects}, nil
}

// parseManifests parses YAML content into a slice of client.Object.
// For types registered in the scheme, it returns typed objects (e.g., *appsv1.Deployment).
// For unknown types (e.g., CRDs), it falls back to *unstructured.Unstructured.
func parseManifests(decoder runtime.Decoder, content []byte) ([]client.Object, error) {
	var objects []client.Object

	// Split multi-document YAML into individual documents
	reader := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read YAML document: %w", err)
		}

		// Skip empty documents
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}

		// Try to decode into a typed object using the scheme
		obj, _, err := decoder.Decode(doc, nil, nil)
		if err != nil {
			// If the type is not registered in the scheme, fall back to unstructured
			if runtime.IsNotRegisteredError(err) {
				u := &unstructured.Unstructured{}
				if err := yaml.Unmarshal(doc, &u.Object); err != nil {
					return nil, fmt.Errorf("failed to decode unstructured manifest: %w", err)
				}
				if len(u.Object) == 0 {
					continue
				}
				objects = append(objects, u)
				continue
			}
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}

		// Cast to client.Object
		clientObj, ok := obj.(client.Object)
		if !ok {
			return nil, fmt.Errorf("decoded object does not implement client.Object: %T", obj)
		}

		objects = append(objects, clientObj)
	}

	return objects, nil
}

// deepCopyObjects returns deep copies of the given objects.
func deepCopyObjects(objects []client.Object) []client.Object {
	copies := make([]client.Object, len(objects))
	for i, obj := range objects {
		copies[i] = obj.DeepCopyObject().(client.Object)
	}
	return copies
}

// GetForComponent returns deep copies of objects for a component.
// Deep copies are returned to prevent mutation of stored objects.
func (s *ObjectStore) GetForComponent(component Component) ([]client.Object, error) {
	objects, ok := s.objects[component]
	if !ok {
		return nil, fmt.Errorf("unknown component: %s", component)
	}
	return deepCopyObjects(objects), nil
}

// GetCRDNamesForComponent returns the names of CustomResourceDefinitions
// that are part of the component's manifests. Used to watch CRDs and enqueue
// the component's CR when a managed CRD is deleted out of band.
func (s *ObjectStore) GetCRDNamesForComponent(component Component) ([]string, error) {
	objects, ok := s.objects[component]
	if !ok {
		return nil, fmt.Errorf("unknown component: %s", component)
	}
	var names []string
	for _, obj := range objects {
		if kubernetes.IsCustomResourceDefinition(obj) {
			names = append(names, obj.GetName())
		}
	}
	return names, nil
}

// ParsedManifestInfo contains parsed manifest information.
type ParsedManifestInfo struct {
	Component Component
	Objects   []client.Object
}

// Walk iterates over all components and calls the provided function for each.
// Deep copies of objects are returned to prevent mutation of stored objects.
func (s *ObjectStore) Walk(fn func(info ParsedManifestInfo) error) error {
	for _, component := range AllComponents() {
		objects := s.objects[component]
		info := ParsedManifestInfo{
			Component: component,
			Objects:   deepCopyObjects(objects),
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}
