package metricsintegration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

const (
	metricsReadyTimeout  = 5 * time.Minute
	metricsReadyInterval = 2 * time.Second
)

var (
	metricsCatalog *metricsauth.Catalog
	repoRoot       string
)

var _ = BeforeSuite(func() {
	Expect(initKubernetesClient()).To(Succeed())
	Expect(kubeClient).NotTo(BeNil())
	Expect(kubeREST).NotTo(BeNil())

	loadMetricsCatalog()

	ctx := context.Background()
	rbacPath := metricsauth.DefaultScraperRBACPath(repoRoot)
	Expect(metricsauth.ApplyManifests(ctx, kubeClient, rbacPath)).To(Succeed())
})

func loadMetricsCatalog() {
	if metricsCatalog != nil {
		return
	}

	var err error
	repoRoot = os.Getenv("KONFLUX_REPO_ROOT")
	if repoRoot == "" {
		repoRoot, err = findRepoRoot()
		if err != nil {
			panic(fmt.Sprintf("metrics catalog: find repo root: %v", err))
		}
	}

	catalogPath := metricsauth.DefaultCatalogPath(repoRoot)
	metricsCatalog, err = metricsauth.LoadCatalog(catalogPath)
	if err != nil {
		panic(fmt.Sprintf("metrics catalog: load %s: %v", catalogPath, err))
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "test", "fixtures", "metrics-targets.yaml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
