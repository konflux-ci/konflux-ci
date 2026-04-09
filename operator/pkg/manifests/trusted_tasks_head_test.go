package manifests

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8syaml "k8s.io/apimachinery/pkg/util/yaml"
)

// trustedTasksYAML documents the Konflux acceptable-bundles OCI artifact
// (see quay.io/konflux-ci/tekton-catalog/data-acceptable-bundles).
type trustedTasksYAML struct {
	TrustedTasks map[string][]trustedTaskEntry `yaml:"trusted_tasks"`
}

type trustedTaskEntry struct {
	Ref       string `yaml:"ref"`
	ExpiresOn string `yaml:"expires_on"`
}

type buildPipelineConfig struct {
	Pipelines []struct {
		Name   string `yaml:"name"`
		Bundle string `yaml:"bundle"`
	} `yaml:"pipelines"`
}

// TestOnboardingPipelineTaskBundlesMatchTrustedTasksCatalogHead ensures every Tekton task bundle
// referenced by onboarding pipelines in the embedded build-service manifest uses the current catalog
// head digest for its oci:// key (trusted_tasks[key][0]: ref only, no expires_on). This catches
// drift when pipelines still pin older digests that appear only under expires_on rows.
func TestOnboardingPipelineTaskBundlesMatchTrustedTasksCatalogHead(t *testing.T) {
	g := gomega.NewWithT(t)

	buildYAML, err := GetManifest(BuildService)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "GetManifest(BuildService)")
	if err != nil {
		return
	}
	ecYAML, err := GetManifest(EnterpriseContract)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "GetManifest(EnterpriseContract)")
	if err != nil {
		return
	}

	cm, err := findConfigMap(buildYAML, "build-pipeline-config")
	g.Expect(err).NotTo(gomega.HaveOccurred(), "find ConfigMap build-pipeline-config")
	if err != nil {
		return
	}
	configStr, _, err := unstructured.NestedString(cm.Object, "data", "config.yaml")
	g.Expect(err).NotTo(gomega.HaveOccurred(), "read data.config.yaml from build-pipeline-config")
	g.Expect(configStr).NotTo(gomega.BeEmpty(), "build-pipeline-config data.config.yaml")
	if err != nil || configStr == "" {
		return
	}
	var cfg buildPipelineConfig
	err = yaml.Unmarshal([]byte(configStr), &cfg)
	g.Expect(err).NotTo(gomega.HaveOccurred(), "parse build pipeline config.yaml")
	if err != nil {
		return
	}

	bundlesRef, err := extractAcceptableBundlesImageRef(ecYAML)
	g.Expect(err).NotTo(gomega.HaveOccurred(),
		"extract data-acceptable-bundles image ref from default EnterpriseContractPolicy")
	if err != nil {
		return
	}

	trusted := fetchTrustedTasksMapEventually(g, bundlesRef)

	for _, p := range cfg.Pipelines {
		if p.Bundle == "" {
			continue
		}
		t.Run(p.Name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			pipelineDoc := pullPipelineDocumentEventually(g, p.Bundle)
			pipeCtx := onboardingPipelineFailureContext(p.Name, p.Bundle)

			tasks, err := collectBundlesResolverTaskRefs(pipelineDoc)
			g.Expect(err).NotTo(gomega.HaveOccurred(),
				"collect bundles resolver task refs from pipeline %q\n    %s", p.Name, pipeCtx)
			if err != nil {
				return
			}

			for taskName, bundleParam := range tasks {
				img, digest, err := splitImageAndDigest(bundleParam)
				g.Expect(err).NotTo(gomega.HaveOccurred(),
					"task %q: parse bundle reference %q\n    %s", taskName, bundleParam, pipeCtx)
				if err != nil {
					continue
				}
				ociKey := "oci://" + img

				g.Expect(trusted).To(gomega.HaveKey(ociKey),
					"task %q: bundle %q requires trusted_tasks[%s] (from pinned data-acceptable-bundles)\n    %s",
					taskName, bundleParam, ociKey, pipeCtx)
				entries := trusted[ociKey]
				g.Expect(entries).NotTo(gomega.BeEmpty(),
					"task %q: trusted_tasks[%s] must list at least one ref\n    %s",
					taskName, ociKey, pipeCtx)
				if len(entries) == 0 {
					continue
				}

				g.Expect(entries[0].ExpiresOn).To(gomega.BeEmpty(),
					"task %q: trusted_tasks[%s][0] must be the non-expiring head (unexpected expires_on %q)\n    %s",
					taskName, ociKey, entries[0].ExpiresOn, pipeCtx)

				g.Expect(digest).To(gomega.Equal(entries[0].Ref),
					"task %q: pipeline pins digest that is not the trusted_tasks catalog head for %s\n"+
						"    bundle param: %s\n"+
						"    digest in pipeline: %s\n"+
						"    catalog head ref:   %s\n"+
						"    %s",
					taskName, ociKey, bundleParam, digest, entries[0].Ref, pipeCtx)
			}
		})
	}
}

func findConfigMap(manifest []byte, name string) (*unstructured.Unstructured, error) {
	docs, err := splitYAMLDocuments(manifest)
	if err != nil {
		return nil, err
	}
	for _, doc := range docs {
		var u unstructured.Unstructured
		if err := yaml.Unmarshal(doc, &u.Object); err != nil {
			continue
		}
		if u.GetKind() == "ConfigMap" && u.GetName() == name {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("ConfigMap %q not found", name)
}

func splitYAMLDocuments(manifest []byte) ([][]byte, error) {
	var docs [][]byte
	reader := k8syaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(manifest)))
	for {
		doc, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func extractAcceptableBundlesImageRef(manifest []byte) (string, error) {
	docs, err := splitYAMLDocuments(manifest)
	if err != nil {
		return "", err
	}
	prefix := "oci::"
	suffix := "data-acceptable-bundles"
	for _, doc := range docs {
		var u unstructured.Unstructured
		if err := yaml.Unmarshal(doc, &u.Object); err != nil {
			continue
		}
		if u.GetKind() != "EnterpriseContractPolicy" || u.GetName() != "default" {
			continue
		}
		sources, found, err := unstructured.NestedSlice(u.Object, "spec", "sources")
		if err != nil || !found {
			continue
		}
		for _, src := range sources {
			sm, ok := src.(map[string]interface{})
			if !ok {
				continue
			}
			data, ok := sm["data"].([]interface{})
			if !ok {
				continue
			}
			for _, row := range data {
				s, ok := row.(string)
				if !ok || !strings.HasPrefix(s, prefix) {
					continue
				}
				if !strings.Contains(s, suffix) {
					continue
				}
				// oci::quay.io/.../data-acceptable-bundles:latest@sha256:...
				rest := strings.TrimPrefix(s, prefix)
				return rest, nil
			}
		}
	}
	return "", fmt.Errorf("default EnterpriseContractPolicy data entry for data-acceptable-bundles not found")
}

const (
	registryEventuallyTimeout = 2 * time.Minute
	registryEventuallyPolling = 500 * time.Millisecond
)

// fetchTrustedTasksMapEventually polls fetchTrustedTasksMapOnce until success or timeout.
// Non-retriable errors (bad ref, wrong content) abort immediately via StopTrying.
func fetchTrustedTasksMapEventually(g *gomega.WithT, imageRef string) map[string][]trustedTaskEntry {
	var trusted map[string][]trustedTaskEntry
	g.Eventually(func() error {
		var err error
		trusted, err = fetchTrustedTasksMapOnce(imageRef)
		if err != nil && !isRetriableRegistryError(err) {
			return gomega.StopTrying("fetch trusted_tasks: non-retriable registry error").Wrap(err)
		}
		return err
	}).WithTimeout(registryEventuallyTimeout).WithPolling(registryEventuallyPolling).Should(gomega.Succeed())
	return trusted
}

func fetchTrustedTasksMapOnce(imageRef string) (map[string][]trustedTaskEntry, error) {
	img, err := crane.Pull(imageRef)
	if err != nil {
		return nil, fmt.Errorf("crane.Pull %q: %w", imageRef, err)
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		rc, err := layer.Uncompressed()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}
		var root trustedTasksYAML
		if err := yaml.Unmarshal(b, &root); err != nil {
			continue
		}
		if len(root.TrustedTasks) > 0 {
			return root.TrustedTasks, nil
		}
	}
	return nil, fmt.Errorf("no trusted_tasks YAML in layers of %q", imageRef)
}

func pullPipelineDocumentEventually(g *gomega.WithT, imageRef string) map[string]interface{} {
	var doc map[string]interface{}
	g.Eventually(func() error {
		var err error
		doc, err = pullPipelineDocumentOnce(imageRef)
		if err != nil && !isRetriableRegistryError(err) {
			return gomega.StopTrying("pull pipeline bundle: non-retriable registry error").Wrap(err)
		}
		return err
	}).WithTimeout(registryEventuallyTimeout).WithPolling(registryEventuallyPolling).Should(gomega.Succeed())
	return doc
}

func pullPipelineDocumentOnce(imageRef string) (map[string]interface{}, error) {
	img, err := crane.Pull(imageRef)
	if err != nil {
		return nil, fmt.Errorf("crane.Pull %q: %w", imageRef, err)
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}
	for _, layer := range layers {
		rc, err := layer.Uncompressed()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}
		doc, err := parsePipelineFromLayerBlob(b)
		if err == nil && doc != nil {
			return doc, nil
		}
	}
	return nil, fmt.Errorf("no Pipeline document in layers of %q", imageRef)
}

func isRetriableRegistryError(err error) bool {
	if err == nil {
		return false
	}
	var terr *transport.Error
	if errors.As(err, &terr) {
		return terr.Temporary()
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	return false
}

func parsePipelineFromLayerBlob(b []byte) (map[string]interface{}, error) {
	// layer.Uncompressed() from go-containerregistry returns gunzipped bytes (tar) for Tekton bundles.
	if doc, err := scanTarForPipeline(bytes.NewReader(b)); err == nil && doc != nil {
		return doc, nil
	}
	gr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("no Pipeline in layer")
	}
	defer func() {
		_ = gr.Close() //nolint:errcheck // best-effort close after reads from gzip.Reader
	}()
	if doc, err := scanTarForPipeline(gr); err == nil && doc != nil {
		return doc, nil
	}
	return nil, fmt.Errorf("no Pipeline in layer")
}

func scanTarForPipeline(r io.Reader) (map[string]interface{}, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		var doc map[string]interface{}
		if json.Unmarshal(buf, &doc) == nil && pipelineDocumentKind(doc) {
			return doc, nil
		}
		doc = nil
		if yaml.Unmarshal(buf, &doc) == nil && pipelineDocumentKind(doc) {
			return doc, nil
		}
	}
	return nil, fmt.Errorf("no Pipeline in archive")
}

func pipelineDocumentKind(doc map[string]interface{}) bool {
	if doc == nil {
		return false
	}
	k, ok := doc["kind"].(string)
	return ok && k == "Pipeline"
}

func collectBundlesResolverTaskRefs(pipeline map[string]interface{}) (map[string]string, error) {
	spec, ok := pipeline["spec"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("pipeline has no spec")
	}
	out := make(map[string]string)
	for _, section := range []string{"tasks", "finally"} {
		raw, ok := spec[section].([]interface{})
		if !ok {
			continue
		}
		for _, item := range raw {
			tm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := tm["name"].(string)
			taskRef, ok := tm["taskRef"].(map[string]interface{})
			if !ok {
				continue
			}
			if resolver, _ := taskRef["resolver"].(string); resolver != "bundles" {
				continue
			}
			params, ok := taskRef["params"].([]interface{})
			if !ok {
				continue
			}
			bundle := paramValue(params, "bundle")
			if bundle == "" {
				continue
			}
			if name == "" {
				name = bundle
			}
			out[name] = bundle
		}
	}
	return out, nil
}

func paramValue(params []interface{}, wantName string) string {
	for _, p := range params {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		n, _ := pm["name"].(string)
		if n != wantName {
			continue
		}
		v, _ := pm["value"].(string)
		return v
	}
	return ""
}

// onboardingPipelineFailureContext returns a single-line summary for test failures: pipeline name,
// parsed bundle digest, and full bundle ref from build-pipeline-config.
func onboardingPipelineFailureContext(pipelineName, bundleRef string) string {
	digest := "(could not parse digest from bundle ref)"
	if _, d, err := splitImageAndDigest(bundleRef); err == nil {
		digest = d
	}
	return fmt.Sprintf(
		"onboarding pipeline: name=%q digest=%s bundle=%s",
		pipelineName, digest, bundleRef)
}

func splitImageAndDigest(bundleParam string) (image, digest string, err error) {
	// quay.io/org/repo/name:tag@sha256:...
	at := strings.LastIndex(bundleParam, "@")
	if at <= 0 || at == len(bundleParam)-1 {
		return "", "", fmt.Errorf("missing @digest")
	}
	digest = bundleParam[at+1:]
	if !strings.HasPrefix(digest, "sha256:") {
		return "", "", fmt.Errorf("expected sha256 digest, got %q", digest)
	}
	image = bundleParam[:at]
	if image == "" {
		return "", "", fmt.Errorf("empty image")
	}
	return image, digest, nil
}
