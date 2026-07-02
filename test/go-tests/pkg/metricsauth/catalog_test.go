package metricsauth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeCatalogFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "metrics-targets.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadCatalog_TargetGroups(t *testing.T) {
	dir := t.TempDir()
	path := writeCatalogFile(t, dir, `
scraper:
  namespace: test-ns
  serviceAccount: prometheus
targets:
  - id: konflux-operator
    group: operator
    namespace: konflux-operator
    service: metrics
    scheme: https
    portName: https
    port: 8443
    path: /metrics
    metricsReaderClusterRole: konflux-operator-metrics-reader
    bodyMustMatchAny:
      - workqueue_
  - id: build-service
    namespace: build-service
    service: metrics
    scheme: https
    portName: https
    port: 8443
    path: /metrics
    metricsReaderClusterRole: build-service-metrics-reader
    bodyMustMatchAny:
      - workqueue_
`)

	catalog, err := LoadCatalog(path)
	require.NoError(t, err)
	require.Len(t, catalog.Targets, 2)

	assert.Equal(t, TargetGroupOperator, catalog.Targets[0].LabelGroup())
	assert.Equal(t, TargetGroupComponent, catalog.Targets[1].LabelGroup())
}

func TestLoadCatalog_InvalidGroup(t *testing.T) {
	dir := t.TempDir()
	path := writeCatalogFile(t, dir, `
scraper:
  namespace: test-ns
  serviceAccount: prometheus
targets:
  - id: bad
    group: unknown
    namespace: ns
    service: metrics
    portName: https
    port: 8443
    path: /metrics
    metricsReaderClusterRole: role
    bodyMustMatchAny:
      - workqueue_
`)

	_, err := LoadCatalog(path)
	assert.Error(t, err)
}
