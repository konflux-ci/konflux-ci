package overrides

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func newTestRunner(g *WithT, root string, o Overrides) *Runner {
	r, err := NewRunner(
		filepath.Join(root, "operator", "upstream-kustomizations"),
		filepath.Join(root, "operator", "pkg", "manifests"),
		filepath.Join(root, ".tmp"),
		o,
	)
	g.Expect(err).ToNot(HaveOccurred())
	return r
}

func TestParseAndValidateFromYAML(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	_, err := ParseAndValidateFromYAML(`
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
      remote:
        repo: https://github.com/konflux-ci/segment-bridge
        ref: abc123
  images:
    - orig: quay.io/konflux-ci/segment-bridge
      replacement: quay.io/example/segment-bridge:pr
`)
	g.Expect(err).ToNot(HaveOccurred())
}

func TestParseAndValidateFromYAMLRejectsInvalidRule(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	_, err := ParseAndValidateFromYAML(`
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
  images: []
`)
	g.Expect(err).To(HaveOccurred())
}

func TestApplyImageOverridesInManifests(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	manifestDir := filepath.Join(root, "operator", "pkg", "manifests", "segment-bridge")
	g.Expect(os.MkdirAll(manifestDir, 0o755)).To(Succeed())
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: segment-bridge
spec:
  template:
    spec:
      containers:
        - name: app
          image: quay.io/konflux-ci/segment-bridge:old
`
	path := filepath.Join(manifestDir, "manifests.yaml")
	g.Expect(os.WriteFile(path, []byte(manifest), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{Orig: "quay.io/konflux-ci/segment-bridge", Replacement: "quay.io/example/segment-bridge:new"},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.applyImageOverridesInManifests(
		filepath.Join(root, "operator", "pkg", "manifests"),
	)).To(Succeed())
	g.Expect(r.Stats().ManifestYAMLsImageTextReplaced).To(Equal(1))
	got, err := os.ReadFile(path)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(ContainSubstring("quay.io/example/segment-bridge:new"))
}

func TestApplyGitRulesToKustomizationWithRemote(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	kPath := filepath.Join(root, "kustomization.yaml")
	src := `resources:
  - https://github.com/konflux-ci/segment-bridge/config/default?ref=old
images:
  - name: quay.io/konflux-ci/segment-bridge
    newTag: old
`
	g.Expect(os.WriteFile(kPath, []byte(src), 0o644)).To(Succeed())
	r := newTestRunner(g, root, Overrides{
		{
			Name: "segment-bridge",
			Git: []GitRule{
				{
					SourceRepo: "konflux-ci/segment-bridge",
					Remote:     &RemoteGit{Repo: "konflux-ci/segment-bridge", Ref: "newref"},
				},
			},
		},
	})
	written, err := r.applyGitRulesToKustomization(kPath, r.Overrides[0].Git)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(written).To(BeTrue())
	got, err := os.ReadFile(kPath)
	g.Expect(err).ToNot(HaveOccurred())
	text := string(got)
	g.Expect(text).To(ContainSubstring("https://github.com/konflux-ci/segment-bridge/config/default?ref=newref"))
	g.Expect(text).To(ContainSubstring("newTag: newref"))
}

func TestRunnerApplyWithImageOverrideOnly(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	upstreamSegBridge := filepath.Join(root, "operator", "upstream-kustomizations", "segment-bridge")
	g.Expect(os.MkdirAll(upstreamSegBridge, 0o755)).To(Succeed())
	manifestDir := filepath.Join(root, "operator", "pkg", "manifests", "segment-bridge")
	g.Expect(os.MkdirAll(manifestDir, 0o755)).To(Succeed())

	manifestPath := filepath.Join(manifestDir, "manifests.yaml")
	g.Expect(os.WriteFile(manifestPath, []byte(`apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: app
          image: quay.io/konflux-ci/segment-bridge:old
`), 0o644)).To(Succeed())

	r := newTestRunner(g, root, Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{
					Orig:        "quay.io/konflux-ci/segment-bridge",
					Replacement: "quay.io/example/segment-bridge:new",
				},
			},
		},
	})
	g.Expect(r.Apply()).To(Succeed())
	g.Expect(r.Stats()).To(Equal(ApplyStats{
		ManifestYAMLsImageTextReplaced: 1,
	}))

	gotManifest, err := os.ReadFile(manifestPath)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(gotManifest)).To(ContainSubstring("quay.io/example/segment-bridge:new"))

	componentSourcesPath := filepath.Join(root, ".tmp", "component-sources.json")
	componentSources, err := os.ReadFile(componentSourcesPath)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(componentSources)).To(ContainSubstring(`"name": "segment-bridge"`))
}

func TestRunnerGitSummaryLines(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	r := Runner{
		Overrides: Overrides{
			{
				Name: "segment-bridge",
				Git: []GitRule{
					{
						SourceRepo: "konflux-ci/segment-bridge",
						Remote:     &RemoteGit{Repo: "konflux-ci/segment-bridge", Ref: "abc123"},
					},
					{
						SourceRepo: "other/org",
						LocalPath:  "/tmp/local-checkout",
					},
				},
			},
		},
	}
	g.Expect(r.GitSummaryLines()).To(Equal([]string{
		"  [segment-bridge] konflux-ci/segment-bridge -> https://github.com/konflux-ci/segment-bridge?ref=abc123",
		"  [segment-bridge] other/org -> local /tmp/local-checkout",
	}))
}

func TestRunnerSummaryLines(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	r := Runner{
		Overrides: Overrides{
			{
				Name: "segment-bridge",
				Images: []ImageOverride{
					{
						Orig:        "quay.io/konflux-ci/segment-bridge",
						Replacement: "quay.io/example/segment-bridge:new",
					},
				},
			},
		},
	}
	g.Expect(r.SummaryLines()).To(Equal([]string{
		"  quay.io/konflux-ci/segment-bridge -> quay.io/example/segment-bridge:new",
	}))
}
