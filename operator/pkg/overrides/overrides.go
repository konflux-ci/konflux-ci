package overrides

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"sigs.k8s.io/yaml"
	yamlv3 "sigs.k8s.io/yaml/goyaml.v3"
)

type Overrides []ComponentOverride

// ComponentOverride describes per-component git and image substitutions.
type ComponentOverride struct {
	Name   string          `json:"name" yaml:"name"`
	Git    []GitRule       `json:"git" yaml:"git"`
	Images []ImageOverride `json:"images" yaml:"images"`
}

// GitRule maps resources from sourceRepo to either a remote repo/ref or localPath.
type GitRule struct {
	SourceRepo string     `json:"sourceRepo" yaml:"sourceRepo"`
	Remote     *RemoteGit `json:"remote,omitempty" yaml:"remote,omitempty"`
	LocalPath  string     `json:"localPath,omitempty" yaml:"localPath,omitempty"`
}

// RemoteGit defines a replacement remote repository and reference.
type RemoteGit struct {
	Repo string `json:"repo" yaml:"repo"`
	Ref  string `json:"ref" yaml:"ref"`
}

// ImageOverride replaces a released image reference with a replacement image.
type ImageOverride struct {
	Orig        string `json:"orig" yaml:"orig"`
	Replacement string `json:"replacement" yaml:"replacement"`
}

// ApplyStats summarizes filesystem writes performed by the last Apply() call.
type ApplyStats struct {
	GitKustomizationsUpdated       int // kustomization.yaml files rewritten for git URL/tag rules
	KustomizationImagesPatched     int // kustomization.yaml entries: digest stripped, name/tag from overrides
	ManifestYAMLsImageTextReplaced int // manifests.yaml files with container image strings replaced
	ComponentsRebuilt              int // components re-built with kustomize into pkg/manifests
}

// Runner applies validated overrides to upstream kustomizations and generated manifests.
type Runner struct {
	UpstreamDir  string
	ManifestsDir string
	TmpDir       string
	Overrides    Overrides

	applyStats ApplyStats
}

// ParseAndValidateFromYAML parses override YAML and validates schema constraints.
func ParseAndValidateFromYAML(content string) (Overrides, error) {
	var o Overrides
	if err := yaml.Unmarshal([]byte(content), &o); err != nil {
		return nil, fmt.Errorf("parse overrides yaml: %w", err)
	}
	if err := validateOverrides(o); err != nil {
		return nil, err
	}
	return o, nil
}

// NewRunner creates a Runner with explicit directories after validating overrides.
func NewRunner(upstreamDir, manifestsDir, tmpDir string, overrides Overrides) (*Runner, error) {
	if err := validateOverrides(overrides); err != nil {
		return nil, err
	}
	if strings.TrimSpace(upstreamDir) == "" {
		return nil, fmt.Errorf("upstreamDir is required")
	}
	if strings.TrimSpace(manifestsDir) == "" {
		return nil, fmt.Errorf("manifestsDir is required")
	}
	if strings.TrimSpace(tmpDir) == "" {
		return nil, fmt.Errorf("tmpDir is required")
	}

	absUpstreamDir, err := filepath.Abs(upstreamDir)
	if err != nil {
		return nil, fmt.Errorf("resolve upstreamDir: %w", err)
	}
	absManifestsDir, err := filepath.Abs(manifestsDir)
	if err != nil {
		return nil, fmt.Errorf("resolve manifestsDir: %w", err)
	}
	absTmpDir, err := filepath.Abs(tmpDir)
	if err != nil {
		return nil, fmt.Errorf("resolve tmpDir: %w", err)
	}
	return &Runner{
		UpstreamDir:  absUpstreamDir,
		ManifestsDir: absManifestsDir,
		TmpDir:       absTmpDir,
		Overrides:    overrides,
	}, nil
}

// Apply executes override transformations and writes resulting manifest updates.
func (r *Runner) Apply() error {
	r.applyStats = ApplyStats{}
	if err := os.MkdirAll(r.TmpDir, 0o755); err != nil {
		return fmt.Errorf("create .tmp: %w", err)
	}
	if err := r.writeComponentSources(); err != nil {
		return err
	}

	workUpstream := r.UpstreamDir
	componentsWithGit := r.componentsWithGitRules()
	if len(componentsWithGit) > 0 {
		workUpstream = filepath.Join(r.TmpDir, "upstream-kustomizations")
		if err := os.RemoveAll(workUpstream); err != nil {
			return fmt.Errorf("cleanup temp upstream: %w", err)
		}
		if err := copyDir(r.UpstreamDir, workUpstream); err != nil {
			return fmt.Errorf("copy upstream-kustomizations: %w", err)
		}
		if err := r.applyGitRules(workUpstream); err != nil {
			return err
		}
	}

	if err := r.applyImageOverridesInKustomizations(workUpstream); err != nil {
		return err
	}
	if len(componentsWithGit) > 0 {
		if err := r.rebuildManifests(workUpstream, r.ManifestsDir, componentsWithGit); err != nil {
			return err
		}
	}
	if err := r.applyImageOverridesInManifests(r.ManifestsDir); err != nil {
		return err
	}
	return nil
}

// GitSummaryLines returns human-readable git override lines for logs (from config).
func (r *Runner) GitSummaryLines() []string {
	var lines []string
	for _, c := range r.Overrides {
		for _, g := range c.Git {
			src, err := normalizeOrgRepo(g.SourceRepo, "sourceRepo")
			if err != nil {
				continue
			}
			switch {
			case g.Remote != nil:
				rr, err := normalizeOrgRepo(g.Remote.Repo, "remote.repo")
				if err != nil {
					continue
				}
				lines = append(lines, fmt.Sprintf("  [%s] %s -> https://github.com/%s?ref=%s", c.Name, src, rr, g.Remote.Ref))
			case strings.TrimSpace(g.LocalPath) != "":
				lines = append(lines, fmt.Sprintf("  [%s] %s -> local %s", c.Name, src, strings.TrimSpace(g.LocalPath)))
			}
		}
	}
	return lines
}

// SummaryLines returns human-readable image replacement lines for logs (from config).
func (r *Runner) SummaryLines() []string {
	var lines []string
	for _, c := range r.Overrides {
		for _, img := range c.Images {
			lines = append(lines, fmt.Sprintf("  %s -> %s", img.Orig, img.Replacement))
		}
	}
	return lines
}

// Stats returns filesystem write counters from the last Apply() (zero if Apply never completed successfully).
func (r *Runner) Stats() ApplyStats {
	return r.applyStats
}

func (r *Runner) writeComponentSources() error {
	type gitOnly struct {
		Name string    `json:"name"`
		Git  []GitRule `json:"git"`
	}
	out := make([]gitOnly, 0, len(r.Overrides))
	for _, c := range r.Overrides {
		out = append(out, gitOnly{Name: c.Name, Git: c.Git})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal component sources: %w", err)
	}
	path := filepath.Join(r.TmpDir, "component-sources.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write component sources: %w", err)
	}
	return nil
}

func (r *Runner) componentsWithGitRules() []string {
	var names []string
	for _, c := range r.Overrides {
		if len(c.Git) > 0 {
			names = append(names, c.Name)
		}
	}
	return names
}

func (r *Runner) applyGitRules(upstreamDir string) error {
	for _, component := range r.Overrides {
		if len(component.Git) == 0 {
			continue
		}
		componentDir := filepath.Join(upstreamDir, component.Name)
		if _, err := os.Stat(componentDir); err != nil {
			continue
		}
		err := filepath.WalkDir(componentDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if d.Name() != "kustomization.yaml" && d.Name() != "kustomization.yml" {
				return nil
			}
			written, err := r.applyGitRulesToKustomization(path, component.Git)
			if err != nil {
				return err
			}
			if written {
				r.applyStats.GitKustomizationsUpdated++
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) applyGitRulesToKustomization(path string, rules []GitRule) (bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var k map[string]any
	if err := yaml.Unmarshal(content, &k); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	rawResources, ok := k["resources"].([]any)
	if !ok {
		return false, nil
	}

	remoteRefs := map[string]struct{}{}
	// GitHub org/repo lines (from resource URLs) that matched a remote git rule — used to scope
	// newTag bumps so unrelated images in the same kustomization are not rewritten.
	remoteSourceOrgRepos := map[string]struct{}{}
	updated := false
	for i, rv := range rawResources {
		resource, ok := rv.(string)
		if !ok || !strings.HasPrefix(resource, "https://github.com/") {
			continue
		}
		orgRepo, suffix, ok := githubURLParts(resource)
		if !ok {
			continue
		}
		rule := firstMatchingRule(orgRepo, rules)
		if rule == nil {
			continue
		}
		var newResource string
		switch {
		case rule.Remote != nil:
			remoteSourceOrgRepos[orgRepo] = struct{}{}
			remoteRepo, err := normalizeOrgRepo(rule.Remote.Repo, "remote.repo")
			if err != nil {
				return false, err
			}
			newResource = fmt.Sprintf("https://github.com/%s%s?ref=%s", remoteRepo, suffix, rule.Remote.Ref)
			remoteRefs[rule.Remote.Ref] = struct{}{}
		case strings.TrimSpace(rule.LocalPath) != "":
			base := filepath.Clean(rule.LocalPath)
			full := filepath.Join(base, strings.TrimPrefix(suffix, "/"))
			info, err := os.Stat(full)
			if err != nil || !info.IsDir() {
				return false, fmt.Errorf("localPath + suffix is not a directory: %s", full)
			}
			kdir := filepath.Dir(path)
			rel, err := filepath.Rel(kdir, full)
			if err != nil {
				return false, fmt.Errorf("compute relative localPath: %w", err)
			}
			newResource = rel
		default:
			return false, fmt.Errorf("rule must set remote or localPath for sourceRepo=%s", rule.SourceRepo)
		}
		if newResource != resource {
			rawResources[i] = newResource
			updated = true
		}
	}
	if updated {
		k["resources"] = rawResources
	}

	if len(remoteRefs) == 1 && len(remoteSourceOrgRepos) > 0 {
		var onlyRef string
		for ref := range remoteRefs {
			onlyRef = ref
		}
		if images, ok := k["images"].([]any); ok {
			for _, iv := range images {
				im, ok := iv.(map[string]any)
				if !ok {
					continue
				}
				if _, hasTag := im["newTag"]; !hasTag {
					continue
				}
				imgRef := imageNameFromMap(im)
				match := false
				for orgRepo := range remoteSourceOrgRepos {
					if imageRefMatchesOrgRepo(imgRef, orgRepo) {
						match = true
						break
					}
				}
				if !match {
					continue
				}
				im["newTag"] = onlyRef
				updated = true
			}
		}
	}

	if !updated {
		return false, nil
	}
	out, err := yaml.Marshal(k)
	if err != nil {
		return false, fmt.Errorf("marshal updated kustomization: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) applyImageOverridesInKustomizations(upstreamDir string) error {
	overrides := r.imageOverrides()
	if len(overrides) == 0 {
		return nil
	}
	return filepath.WalkDir(upstreamDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (d.Name() != "kustomization.yaml" && d.Name() != "kustomization.yml") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc map[string]any
		if err := yaml.Unmarshal(content, &doc); err != nil {
			return nil
		}
		images, ok := doc["images"].([]any)
		if !ok || len(images) == 0 {
			return nil
		}
		hasDigest := false
		for _, iv := range images {
			im, ok := iv.(map[string]any)
			if ok {
				if _, found := im["digest"]; found {
					hasDigest = true
					break
				}
			}
		}
		if !hasDigest {
			return nil
		}
		changed := false
		for _, ov := range overrides {
			newName, newTag, newDigest := parseImageReference(ov.Replacement)
			for _, iv := range images {
				im, ok := iv.(map[string]any)
				if !ok {
					continue
				}
				if imageNameFromMap(im) != ov.Orig {
					continue
				}
				im["newName"] = newName
				if newDigest != "" {
					im["digest"] = newDigest
					delete(im, "newTag")
				} else {
					im["newTag"] = newTag
					delete(im, "digest")
				}
				changed = true
			}
		}
		if !changed {
			return nil
		}
		doc["images"] = images
		out, err := yaml.Marshal(doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, out, 0o644); err != nil {
			return err
		}
		r.applyStats.KustomizationImagesPatched++
		return nil
	})
}

// kustomizeBuildCommand returns a command that renders kustomization at src to stdout.
// Prefer the standalone kustomize CLI when present; otherwise use kubectl's embedded kustomize
// (Tekton deploy-prep copies kubectl onto PATH but does not ship the kustomize binary).
func kustomizeBuildCommand(src string) (*exec.Cmd, string, error) {
	if _, err := exec.LookPath("kustomize"); err == nil {
		return exec.Command("kustomize", "build", src), "kustomize build", nil
	}
	if _, err := exec.LookPath("kubectl"); err == nil {
		return exec.Command("kubectl", "kustomize", src), "kubectl kustomize", nil
	}
	return nil, "", errors.New("kustomize or kubectl is required in PATH to rebuild manifests")
}

func (r *Runner) rebuildManifests(upstreamDir, manifestsDir string, components []string) error {
	for _, component := range components {
		src := filepath.Join(upstreamDir, component)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		destDir := filepath.Join(manifestsDir, component)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			return err
		}
		cmd, label, err := kustomizeBuildCommand(src)
		if err != nil {
			return err
		}
		cmd.Dir = manifestsDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed for %s: %w: %s", label, component, err, string(out))
		}
		dest := filepath.Join(destDir, "manifests.yaml")
		if err := os.WriteFile(dest, out, 0o644); err != nil {
			return err
		}
		r.applyStats.ComponentsRebuilt++
	}
	return nil
}

func (r *Runner) applyImageOverridesInManifests(manifestsDir string) error {
	overrides := r.imageOverrides()
	if len(overrides) == 0 {
		return nil
	}
	return filepath.WalkDir(manifestsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "manifests.yaml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		decoder := yamlv3.NewDecoder(bytes.NewReader(content))
		var docs []*yamlv3.Node
		changed := false
		for {
			var doc yamlv3.Node
			if err := decoder.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				return err
			}
			if doc.Kind == 0 {
				break
			}
			for _, ov := range overrides {
				re := regexp.MustCompile("^" + regexp.QuoteMeta(ov.Orig) + `($|:|@)`)
				if replaceStringNodes(&doc, re, ov.Replacement) {
					changed = true
				}
			}
			docs = append(docs, &doc)
		}
		if !changed {
			return nil
		}
		var out bytes.Buffer
		enc := yamlv3.NewEncoder(&out)
		enc.SetIndent(2)
		for _, doc := range docs {
			if err := enc.Encode(doc); err != nil {
				return err
			}
		}
		_ = enc.Close()
		if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
			return err
		}
		r.applyStats.ManifestYAMLsImageTextReplaced++
		return nil
	})
}

func replaceStringNodes(n *yamlv3.Node, re *regexp.Regexp, replacement string) bool {
	changed := false
	if n.Kind == yamlv3.ScalarNode && n.Tag == "!!str" && re.MatchString(n.Value) {
		n.Value = replacement
		changed = true
	}
	for _, c := range n.Content {
		if replaceStringNodes(c, re, replacement) {
			changed = true
		}
	}
	return changed
}

func (r *Runner) imageOverrides() []ImageOverride {
	var images []ImageOverride
	for _, c := range r.Overrides {
		images = append(images, c.Images...)
	}
	return images
}

func validateOverrides(overrides Overrides) error {
	if len(overrides) == 0 {
		return fmt.Errorf("need non-empty override list")
	}
	seenNames := make(map[string]struct{}, len(overrides))
	for i, c := range overrides {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("entry %d: name is required", i)
		}
		nameKey := strings.TrimSpace(c.Name)
		if _, dup := seenNames[nameKey]; dup {
			return fmt.Errorf("entry %d: duplicate component name %q", i, nameKey)
		}
		seenNames[nameKey] = struct{}{}
		if len(c.Git) == 0 && len(c.Images) == 0 {
			return fmt.Errorf("entry %d (%s): at least one of git/images must be non-empty", i, c.Name)
		}
		for j, g := range c.Git {
			if strings.TrimSpace(g.SourceRepo) == "" {
				return fmt.Errorf("entry %d (%s) git[%d]: sourceRepo is required", i, c.Name, j)
			}
			if _, err := normalizeOrgRepo(g.SourceRepo, "sourceRepo"); err != nil {
				return fmt.Errorf("entry %d (%s) git[%d]: %w", i, c.Name, j, err)
			}
			hasRemote := g.Remote != nil
			hasLocal := strings.TrimSpace(g.LocalPath) != ""
			if hasRemote == hasLocal {
				return fmt.Errorf("entry %d (%s) git[%d]: exactly one of remote/localPath is required", i, c.Name, j)
			}
			if hasRemote {
				if strings.TrimSpace(g.Remote.Repo) == "" || strings.TrimSpace(g.Remote.Ref) == "" {
					return fmt.Errorf("entry %d (%s) git[%d]: remote.repo and remote.ref are required", i, c.Name, j)
				}
				if _, err := normalizeOrgRepo(g.Remote.Repo, "remote.repo"); err != nil {
					return fmt.Errorf("entry %d (%s) git[%d]: %w", i, c.Name, j, err)
				}
			}
		}
		for j, img := range c.Images {
			if strings.TrimSpace(img.Orig) == "" || strings.TrimSpace(img.Replacement) == "" {
				return fmt.Errorf("entry %d (%s) images[%d]: orig/replacement are required", i, c.Name, j)
			}
			repName, _, _ := parseImageReference(img.Replacement)
			if strings.TrimSpace(repName) == "" {
				return fmt.Errorf(
					"entry %d (%s) images[%d]: replacement must be a valid image reference (non-empty name)",
					i, c.Name, j,
				)
			}
		}
	}
	return nil
}

func normalizeOrgRepo(input, field string) (string, error) {
	r := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(input, ".git"), "/"))
	r = strings.TrimPrefix(r, "https://github.com/")
	parts := strings.Split(r, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("%s must be org/repo or https://github.com/org/repo", field)
	}
	return strings.ToLower(parts[0] + "/" + parts[1]), nil
}

func githubURLParts(url string) (orgRepo string, suffix string, ok bool) {
	re := regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)(/[^?]+)(\?ref=[^&]*)?$`)
	m := re.FindStringSubmatch(url)
	if len(m) == 0 {
		return "", "", false
	}
	return strings.ToLower(m[1]), m[2], true
}

func firstMatchingRule(orgRepo string, rules []GitRule) *GitRule {
	for _, r := range rules {
		norm, err := normalizeOrgRepo(r.SourceRepo, "sourceRepo")
		if err != nil {
			continue
		}
		if norm == orgRepo {
			rcopy := r
			return &rcopy
		}
	}
	return nil
}

// parseImageReference splits a container image reference into kustomize newName and either
// newTag or digest (OCI image digest: algorithm + hex after first ':').
func parseImageReference(s string) (name, tag, digest string) {
	s = strings.TrimSpace(s)
	if at := strings.Index(s, "@"); at >= 0 {
		name, rest := s[:at], s[at+1:]
		if isOCIImageDigest(rest) {
			return name, "", rest
		}
		return name, rest, ""
	}
	if i := strings.LastIndex(s, ":"); i > strings.LastIndex(s, "/") {
		return s[:i], s[i+1:], ""
	}
	return s, "latest", ""
}

func isOCIImageDigest(s string) bool {
	// OCI manifest digest: sha256:<64 hex> or sha512:<128 hex>, etc.
	for _, prefix := range []string{"sha256:", "sha512:", "sha384:"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// splitImageReference returns (name, tag) for callers that only distinguish tag vs bare name;
// when the reference uses a digest, tag is empty (digest is not a kustomize newTag).
func splitImageReference(outputImage string) (name, tag string) {
	n, t, d := parseImageReference(outputImage)
	if d != "" {
		return n, ""
	}
	return n, t
}

// imageRefMatchesOrgRepo reports whether a kustomize image name/newName refers to the given
// GitHub org/repo (e.g. quay.io/konflux-ci/segment-bridge matches konflux-ci/segment-bridge).
func imageRefMatchesOrgRepo(imageRef, orgRepo string) bool {
	orgRepo = strings.ToLower(strings.TrimSpace(orgRepo))
	if orgRepo == "" || !strings.Contains(orgRepo, "/") {
		return false
	}
	base, _, _ := parseImageReference(strings.TrimSpace(imageRef))
	base = strings.ToLower(base)
	return strings.HasSuffix(base, "/"+orgRepo)
}

func imageNameFromMap(image map[string]any) string {
	for _, k := range []string{"name", "newName"} {
		if v, ok := image[k].(string); ok {
			return v
		}
	}
	return ""
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if info, statErr := d.Info(); statErr == nil {
			mode = info.Mode()
		}
		return os.WriteFile(target, b, mode)
	})
}
