package overrides

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestParseAndValidateFromYAML_validationFailures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name:    "empty list",
			yaml:    `[]`,
			wantSub: "non-empty override list",
		},
		{
			name: "empty component name",
			yaml: `
- name: "   "
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "name is required",
		},
		{
			name: "empty git and images",
			yaml: `
- name: segment-bridge
  git: []
  images: []
`,
			wantSub: "at least one of git/images must be non-empty",
		},
		{
			name: "missing sourceRepo",
			yaml: `
- name: segment-bridge
  git:
    - remote:
        repo: konflux-ci/segment-bridge
        ref: main
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "sourceRepo is required",
		},
		{
			name: "invalid sourceRepo shape",
			yaml: `
- name: segment-bridge
  git:
    - sourceRepo: not-org-repo-format
      remote:
        repo: konflux-ci/segment-bridge
        ref: main
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "sourceRepo must be org/repo",
		},
		{
			name: "neither remote nor localPath",
			yaml: `
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "exactly one of remote/localPath",
		},
		{
			name: "both remote and localPath",
			yaml: `
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
      localPath: /tmp/x
      remote:
        repo: konflux-ci/segment-bridge
        ref: main
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "exactly one of remote/localPath",
		},
		{
			name: "remote with empty ref",
			yaml: `
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
      remote:
        repo: konflux-ci/segment-bridge
        ref: " "
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "remote.repo and remote.ref are required",
		},
		{
			name: "invalid remote.repo",
			yaml: `
- name: segment-bridge
  git:
    - sourceRepo: konflux-ci/segment-bridge
      remote:
        repo: https://example.com/not-github
        ref: main
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
`,
			wantSub: "remote.repo must be org/repo",
		},
		{
			name: "duplicate component names",
			yaml: `
- name: segment-bridge
  images:
    - orig: quay.io/a/b
      replacement: quay.io/c/d:1
- name: segment-bridge
  images:
    - orig: quay.io/e/f
      replacement: quay.io/g/h:1
`,
			wantSub: "duplicate component name",
		},
		{
			name: "replacement not a usable image ref",
			yaml: `
- name: segment-bridge
  images:
    - orig: quay.io/a/b
      replacement: ":"
`,
			wantSub: "non-empty name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewGomegaWithT(t)
			_, err := ParseAndValidateFromYAML(tc.yaml)
			g.Expect(err).To(HaveOccurred())
			g.Expect(err.Error()).To(ContainSubstring(tc.wantSub))
		})
	}
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

// When a single remote ref is applied, newTag must only track repos tied to rewritten GitHub resources,
// not every images[] entry in the file.
func TestApplyGitRulesToKustomization_remoteNewTagScopedToMatchingImage(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	kPath := filepath.Join(root, "kustomization.yaml")
	src := `resources:
  - https://github.com/konflux-ci/segment-bridge/config/default?ref=old
images:
  - name: quay.io/konflux-ci/segment-bridge
    newTag: old
  - name: quay.io/konflux-ci/other-image
    newTag: keep-me
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
	g.Expect(text).To(ContainSubstring("newTag: keep-me"))
	g.Expect(text).ToNot(ContainSubstring("other-image\n    newTag: newref"))
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

// splitImageReference: tag and bare-name cases; digest refs yield empty tag.
func TestSplitImageReference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		input    string
		wantName string
		wantTag  string
	}{
		{
			name:     "bare name defaults tag to latest",
			input:    "quay.io/org/app",
			wantName: "quay.io/org/app",
			wantTag:  "latest",
		},
		{
			name:     "tag after last slash colon",
			input:    "quay.io/org/app:mytag",
			wantName: "quay.io/org/app",
			wantTag:  "mytag",
		},
		{
			name:     "port in registry",
			input:    "localhost:5000/org/app:v1",
			wantName: "localhost:5000/org/app",
			wantTag:  "v1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotTag := splitImageReference(tc.input)
			if gotName != tc.wantName || gotTag != tc.wantTag {
				t.Fatalf("splitImageReference(%q) = (%q, %q), want (%q, %q)",
					tc.input, gotName, gotTag, tc.wantName, tc.wantTag)
			}
		})
	}
}

// Required: a digest reference must not treat the part after @ as a kustomize tag (newTag).
func TestSplitImageReference_digestNotReturnedAsTag(t *testing.T) {
	t.Parallel()
	input := "quay.io/org/app@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_, gotTag := splitImageReference(input)
	if strings.HasPrefix(gotTag, "sha256:") {
		t.Fatalf(
			"digest after @ must not be returned as tag string "+
				"(belongs in kustomize digest:, not newTag:); got second return %q",
			gotTag,
		)
	}
}

// applyImageOverridesInKustomizations only runs when an images entry already has digest: (see overrides.go).
// Required: digest replacement must set kustomize digest: and must not put the digest in newTag:.
func TestApplyImageOverridesInKustomizations_digestReplacement(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	upstream := filepath.Join(root, "operator", "upstream-kustomizations", "segment-bridge")
	g.Expect(os.MkdirAll(upstream, 0o755)).To(Succeed())

	const oldDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	kustomization := `images:
  - name: quay.io/konflux-ci/segment-bridge
    newName: quay.io/konflux-ci/segment-bridge
    digest: ` + oldDigest + `
`
	kPath := filepath.Join(upstream, "kustomization.yaml")
	g.Expect(os.WriteFile(kPath, []byte(kustomization), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{
					Orig:        "quay.io/konflux-ci/segment-bridge",
					Replacement: "quay.io/example/segment-bridge@" + newDigest,
				},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.applyImageOverridesInKustomizations(
		filepath.Join(root, "operator", "upstream-kustomizations"),
	)).To(Succeed())
	g.Expect(r.Stats().KustomizationImagesPatched).To(Equal(1))

	got, err := os.ReadFile(kPath)
	g.Expect(err).ToNot(HaveOccurred())
	text := string(got)
	g.Expect(text).To(ContainSubstring("newName: quay.io/example/segment-bridge"))
	g.Expect(text).To(ContainSubstring("digest: " + newDigest))
	g.Expect(text).ToNot(
		ContainSubstring("newTag: "+newDigest),
		"digest must not be written to newTag",
	)
}

// Tag-shaped replacement on an entry that used digest: must move to newName/newTag and drop digest:.
func TestApplyImageOverridesInKustomizations_tagReplacementWhenEntryHasDigest(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	upstream := filepath.Join(root, "operator", "upstream-kustomizations", "segment-bridge")
	g.Expect(os.MkdirAll(upstream, 0o755)).To(Succeed())

	const oldDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	kustomization := `images:
  - name: quay.io/konflux-ci/segment-bridge
    newName: quay.io/konflux-ci/segment-bridge
    digest: ` + oldDigest + `
`
	kPath := filepath.Join(upstream, "kustomization.yaml")
	g.Expect(os.WriteFile(kPath, []byte(kustomization), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{
					Orig:        "quay.io/konflux-ci/segment-bridge",
					Replacement: "quay.io/example/segment-bridge:pr-override",
				},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.applyImageOverridesInKustomizations(
		filepath.Join(root, "operator", "upstream-kustomizations"),
	)).To(Succeed())
	g.Expect(r.Stats().KustomizationImagesPatched).To(Equal(1))

	got, err := os.ReadFile(kPath)
	g.Expect(err).ToNot(HaveOccurred())
	text := string(got)
	g.Expect(text).To(ContainSubstring("newName: quay.io/example/segment-bridge"))
	g.Expect(text).To(ContainSubstring("newTag: pr-override"))
	g.Expect(text).ToNot(ContainSubstring("digest: " + oldDigest))
	g.Expect(text).ToNot(ContainSubstring("digest:"), "switching to a tag pin must not leave digest: on this entry")
}

// Only images entries matching orig are updated; others are left unchanged.
func TestApplyImageOverridesInKustomizations_multipleImagesOnlyMatchingRowChanges(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	upstream := filepath.Join(root, "operator", "upstream-kustomizations", "segment-bridge")
	g.Expect(os.MkdirAll(upstream, 0o755)).To(Succeed())

	const firstDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const otherDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	kustomization := `images:
  - name: quay.io/konflux-ci/segment-bridge
    newName: quay.io/konflux-ci/segment-bridge
    digest: ` + firstDigest + `
  - name: quay.io/other/app
    newName: quay.io/other/app
    digest: ` + otherDigest + `
`
	kPath := filepath.Join(upstream, "kustomization.yaml")
	g.Expect(os.WriteFile(kPath, []byte(kustomization), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{
					Orig:        "quay.io/konflux-ci/segment-bridge",
					Replacement: "quay.io/example/segment-bridge:only-first",
				},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.applyImageOverridesInKustomizations(
		filepath.Join(root, "operator", "upstream-kustomizations"),
	)).To(Succeed())
	g.Expect(r.Stats().KustomizationImagesPatched).To(Equal(1))

	got, err := os.ReadFile(kPath)
	g.Expect(err).ToNot(HaveOccurred())
	text := string(got)
	g.Expect(text).To(ContainSubstring("newName: quay.io/example/segment-bridge"))
	g.Expect(text).To(ContainSubstring("newTag: only-first"))
	g.Expect(text).To(ContainSubstring("digest: "+otherDigest), "non-matching image entry must keep its digest")
	g.Expect(text).To(ContainSubstring("name: quay.io/other/app"))
}

// Kustomizations that use newTag only (no digest) are skipped by applyImageOverridesInKustomizations entirely.
func TestApplyImageOverridesInKustomizations_skipsWhenNoDigestInFile(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	upstream := filepath.Join(root, "operator", "upstream-kustomizations", "segment-bridge")
	g.Expect(os.MkdirAll(upstream, 0o755)).To(Succeed())

	kustomization := `images:
  - name: quay.io/konflux-ci/segment-bridge
    newName: quay.io/konflux-ci/segment-bridge
    newTag: old
`
	kPath := filepath.Join(upstream, "kustomization.yaml")
	g.Expect(os.WriteFile(kPath, []byte(kustomization), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{
					Orig:        "quay.io/konflux-ci/segment-bridge",
					Replacement: "quay.io/example/segment-bridge:new",
				},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.applyImageOverridesInKustomizations(
		filepath.Join(root, "operator", "upstream-kustomizations"),
	)).To(Succeed())
	g.Expect(r.Stats().KustomizationImagesPatched).To(Equal(0))

	got, err := os.ReadFile(kPath)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(got)).To(ContainSubstring("newTag: old"))
}

// Manifest replacement substitutes the full replacement string; digest-shaped refs are not split.
func TestApplyImageOverridesInManifests_digestReplacement(t *testing.T) {
	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	manifestDir := filepath.Join(root, "operator", "pkg", "manifests", "segment-bridge")
	g.Expect(os.MkdirAll(manifestDir, 0o755)).To(Succeed())

	const oldImg = "quay.io/konflux-ci/segment-bridge@sha256:" +
		"1111111111111111111111111111111111111111111111111111111111111111"
	const newImg = "quay.io/example/segment-bridge@sha256:" +
		"2222222222222222222222222222222222222222222222222222222222222222"
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: segment-bridge
spec:
  template:
    spec:
      containers:
        - name: app
          image: ` + oldImg + `
`
	path := filepath.Join(manifestDir, "manifests.yaml")
	g.Expect(os.WriteFile(path, []byte(manifest), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "segment-bridge",
			Images: []ImageOverride{
				{Orig: "quay.io/konflux-ci/segment-bridge", Replacement: newImg},
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
	g.Expect(string(got)).To(ContainSubstring(newImg))
	g.Expect(string(got)).ToNot(ContainSubstring(oldImg))
}

func TestParseImageReference(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		input      string
		wantName   string
		wantTag    string
		wantDigest string
	}{
		{
			name:       "digest",
			input:      "quay.io/org/app@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			wantName:   "quay.io/org/app",
			wantTag:    "",
			wantDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name:       "tag then digest uses digest branch",
			input:      "quay.io/org/app:mytag@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			wantName:   "quay.io/org/app:mytag",
			wantTag:    "",
			wantDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name:     "tag only",
			input:    "registry:5000/ns/img:v1.2",
			wantName: "registry:5000/ns/img",
			wantTag:  "v1.2",
		},
		{
			name:     "at without oci digest becomes tag",
			input:    "quay.io/org/app@edge",
			wantName: "quay.io/org/app",
			wantTag:  "edge",
		},
		{
			name:     "bare name defaults latest",
			input:    "quay.io/org/app",
			wantName: "quay.io/org/app",
			wantTag:  "latest",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotTag, gotDigest := parseImageReference(tc.input)
			if gotName != tc.wantName || gotTag != tc.wantTag || gotDigest != tc.wantDigest {
				t.Fatalf("parseImageReference(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.input, gotName, gotTag, gotDigest, tc.wantName, tc.wantTag, tc.wantDigest)
			}
		})
	}
}

func TestNormalizeOrgRepo(t *testing.T) {
	t.Parallel()

	ok := []struct {
		input string
		want  string
	}{
		{"konflux-ci/segment-bridge", "konflux-ci/segment-bridge"},
		{"Konflux-CI/Segment-Bridge", "konflux-ci/segment-bridge"},
		{"https://github.com/konflux-ci/segment-bridge", "konflux-ci/segment-bridge"},
		// `.git` is only trimmed when it is the suffix of the whole string (not `...repo.git/`).
		{"https://github.com/konflux-ci/segment-bridge.git", "konflux-ci/segment-bridge"},
	}
	for _, tc := range ok {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeOrgRepo(tc.input, "sourceRepo")
			if err != nil || got != tc.want {
				t.Fatalf("normalizeOrgRepo(%q) = (%q, %v), want (%q, nil)", tc.input, got, err, tc.want)
			}
		})
	}

	bad := []string{"", "nohost", "org/", "/repo", "https://gitlab.com/a/b"}
	for _, input := range bad {
		t.Run("reject_"+strings.ReplaceAll(input, "/", "_"), func(t *testing.T) {
			t.Parallel()
			_, err := normalizeOrgRepo(input, "sourceRepo")
			if err == nil {
				t.Fatalf("normalizeOrgRepo(%q) wanted error", input)
			}
		})
	}
}

func TestGithubURLParts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		url         string
		wantOrgRepo string
		wantSuffix  string
		ok          bool
	}{
		{
			url:         "https://github.com/konflux-ci/segment-bridge/config/default?ref=abc",
			wantOrgRepo: "konflux-ci/segment-bridge",
			wantSuffix:  "/config/default",
			ok:          true,
		},
		{
			url:         "https://github.com/Konflux-CI/Segment-Bridge/foo?ref=x",
			wantOrgRepo: "konflux-ci/segment-bridge",
			wantSuffix:  "/foo",
			ok:          true,
		},
		{
			url: "https://github.com/org/repo",
			ok:  false,
		},
		{
			url: "https://gitlab.com/org/repo/foo",
			ok:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			t.Parallel()
			orgRepo, suffix, ok := githubURLParts(tc.url)
			if ok != tc.ok {
				t.Fatalf("githubURLParts ok = %v, want %v", ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if orgRepo != tc.wantOrgRepo || suffix != tc.wantSuffix {
				t.Fatalf("githubURLParts(%q) = (%q, %q), want (%q, %q)", tc.url, orgRepo, suffix, tc.wantOrgRepo, tc.wantSuffix)
			}
		})
	}
}

func TestFirstMatchingRule(t *testing.T) {
	t.Parallel()

	rules := []GitRule{
		{SourceRepo: "other/repo", Remote: &RemoteGit{Repo: "x/y", Ref: "r1"}},
		{SourceRepo: "konflux-ci/segment-bridge", Remote: &RemoteGit{Repo: "fork/segment-bridge", Ref: "r2"}},
	}
	got := firstMatchingRule("konflux-ci/segment-bridge", rules)
	if got == nil || got.Remote == nil || got.Remote.Ref != "r2" {
		t.Fatalf("firstMatchingRule: got %#v", got)
	}
	if firstMatchingRule("missing/repo", rules) != nil {
		t.Fatal("expected nil for unmatched org/repo")
	}
}

// Exercise Apply() git rules + kustomize rebuild (skipped when neither kustomize nor kubectl is on PATH).
func TestRunnerApply_gitLocalPath_rebuildsManifests(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		if _, err2 := exec.LookPath("kubectl"); err2 != nil {
			t.Skip("kustomize or kubectl required for rebuild step")
		}
	}

	t.Parallel()
	g := NewGomegaWithT(t)

	root := t.TempDir()
	localSeg := filepath.Join(root, "local-seg", "config", "default")
	g.Expect(os.MkdirAll(localSeg, 0o755)).To(Succeed())
	localKustom := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`
	g.Expect(os.WriteFile(filepath.Join(localSeg, "kustomization.yaml"), []byte(localKustom), 0o644)).To(Succeed())

	upComp := filepath.Join(root, "operator", "upstream-kustomizations", "testcomp")
	g.Expect(os.MkdirAll(upComp, 0o755)).To(Succeed())
	upstreamKustom := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - https://github.com/konflux-ci/segment-bridge/config/default?ref=oldpin
`
	g.Expect(os.WriteFile(filepath.Join(upComp, "kustomization.yaml"), []byte(upstreamKustom), 0o644)).To(Succeed())

	manifestDir := filepath.Join(root, "operator", "pkg", "manifests", "testcomp")
	g.Expect(os.MkdirAll(manifestDir, 0o755)).To(Succeed())
	g.Expect(os.WriteFile(filepath.Join(manifestDir, "manifests.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: placeholder
`), 0o644)).To(Succeed())

	o := Overrides{
		{
			Name: "testcomp",
			Git: []GitRule{
				{
					SourceRepo: "konflux-ci/segment-bridge",
					LocalPath:  filepath.Join(root, "local-seg"),
				},
			},
		},
	}
	r := newTestRunner(g, root, o)
	g.Expect(r.Apply()).To(Succeed())
	g.Expect(r.Stats().GitKustomizationsUpdated).To(BeNumerically(">=", 1))
	g.Expect(r.Stats().ComponentsRebuilt).To(Equal(1))

	// Git edits apply to the temp upstream copy, not the original tree.
	rewritten := filepath.Join(root, ".tmp", "upstream-kustomizations", "testcomp", "kustomization.yaml")
	gotK, err := os.ReadFile(rewritten)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(string(gotK)).ToNot(
		ContainSubstring("https://github.com/"),
		"resource should be rewritten to relative local path",
	)
}
