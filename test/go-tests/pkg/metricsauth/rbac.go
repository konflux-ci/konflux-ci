package metricsauth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultScraperRBACPath returns test/fixtures/metrics-scraper/rbac.yaml under repoRoot.
func DefaultScraperRBACPath(repoRoot string) string {
	return filepath.Join(repoRoot, "test", "fixtures", "metrics-scraper", "rbac.yaml")
}

// ApplyManifests creates objects from a multi-document YAML file.
func ApplyManifests(ctx context.Context, c client.Client, manifestPath string) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifests %s: %w", manifestPath, err)
	}
	docs := strings.Split(string(data), "---")
	for _, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), obj); err != nil {
			return fmt.Errorf("decode manifest document: %w", err)
		}
		if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
			continue
		}
		if err := c.Create(ctx, obj); err != nil {
			if apierrors.IsAlreadyExists(err) {
				continue
			}
			return fmt.Errorf("create %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
	}
	return nil
}
