package build

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/moby/buildkit/frontend/dockerfile/parser"
	"github.com/openshift/library-go/pkg/image/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/github"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/tekton"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

const (
	extraSourceSubDir     = "extra_src_dir"
	rpmSubDir             = "rpm_dir"
	srcTarFileRegex       = "extra-src-[0-9a-f]+.tar"
	shaValueRegex         = "[a-f0-9]{40}"
	tarGzFileRegex        = ".tar.gz$"
	gomodDependencySubDir = "deps/gomod/pkg/mod/cache/download/"
	pipDependencySubDir   = "deps/pip/"
)

func GetBinaryImage(pr *pipeline.PipelineRun) string {
	for _, p := range pr.Spec.Params {
		if p.Name == "output-image" {
			return p.Value.StringVal
		}
	}
	return ""
}

func IsSourceBuildEnabled(pr *pipeline.PipelineRun) bool {
	for _, p := range pr.Status.PipelineSpec.Params {
		if p.Name == "build-source-image" {
			if p.Default.StringVal == "true" {
				return true
			}
		}
	}
	return false
}

func IsHermeticBuildEnabled(pr *pipeline.PipelineRun) bool {
	for _, p := range pr.Spec.Params {
		if p.Name == "hermetic" {
			if p.Value.StringVal == "true" {
				return true
			}
		}
	}
	return false
}

func GetPrefetchValue(pr *pipeline.PipelineRun) string {
	for _, p := range pr.Spec.Params {
		if p.Name == "prefetch-input" {
			return p.Value.StringVal
		}
	}
	return ""
}

func IsSourceFilesExistsInSourceImage(srcImage string, gitUrl string, isHermetic bool, prefetchValue string) (bool, error) {
	//Extract the src image locally
	tmpDir, err := ExtractImage(srcImage)
	defer os.RemoveAll(tmpDir)
	if err != nil {
		return false, err
	}

	// Check atleast one file present under extra_src_dir
	absExtraSourceDirPath := filepath.Join(tmpDir, extraSourceSubDir)
	fileNames, err := utils.GetFileNamesFromDir(absExtraSourceDirPath)
	if err != nil {
		return false, fmt.Errorf("error while getting files: %v", err)
	}
	if len(fileNames) == 0 {
		return false, fmt.Errorf("no tar file found in extra_src_dir, found files %v", fileNames)
	}

	// Get all the extra-src-*.tar files
	extraSrcTarFiles := utils.FilterSliceUsingPattern(srcTarFileRegex, fileNames)
	if len(extraSrcTarFiles) == 0 {
		return false, fmt.Errorf("no tar file found with pattern %s", srcTarFileRegex)
	}
	fmt.Printf("Files found with pattern %s: %v\n", srcTarFileRegex, extraSrcTarFiles)

	//Untar all the extra-src-[0-9]+.tar files
	for _, tarFile := range extraSrcTarFiles {
		absExtraSourceTarPath := filepath.Join(absExtraSourceDirPath, tarFile)
		err = utils.Untar(absExtraSourceDirPath, absExtraSourceTarPath)
		if err != nil {
			return false, fmt.Errorf("error while untaring %s: %v", tarFile, err)
		}
	}

	//Check if application source files exists
	_, err = doAppSourceFilesExist(absExtraSourceDirPath)
	if err != nil {
		return false, err
	}
	// Check the pre-fetch dependency related files
	if isHermetic {
		_, err := IsPreFetchDependenciesFilesExists(gitUrl, absExtraSourceDirPath, isHermetic, prefetchValue)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

// doAppSourceFilesExist checks if there is app source archive included.
// For the builds based on Konflux image, multiple app sources could be included.
func doAppSourceFilesExist(absExtraSourceDirPath string) (bool, error) {
	//Get the file list from extra_src_dir
	fileNames, err := utils.GetFileNamesFromDir(absExtraSourceDirPath)
	if err != nil {
		return false, fmt.Errorf("error while getting files: %v", err)
	}

	//Get the component source with pattern <repo-name>-<git-sha>.tar.gz
	filePatternToFind := "^.+-" + shaValueRegex + tarGzFileRegex
	resultFiles := utils.FilterSliceUsingPattern(filePatternToFind, fileNames)
	if len(resultFiles) == 0 {
		return false, fmt.Errorf("did not found the component source inside extra_src_dir, files found are: %v", fileNames)
	}

	fmt.Println("file names:", fileNames)
	fmt.Println("app sources:", resultFiles)

	for _, sourceGzTarFileName := range resultFiles {
		//Untar the <repo-name>-<git-sha>.tar.gz file
		err = utils.Untar(absExtraSourceDirPath, filepath.Join(absExtraSourceDirPath, sourceGzTarFileName))
		if err != nil {
			return false, fmt.Errorf("error while untaring %s: %v", sourceGzTarFileName, err)
		}

		//Get the file list from extra_src_dir/<repo-name>-<sha>
		sourceGzTarDirName := strings.TrimSuffix(sourceGzTarFileName, ".tar.gz")
		absSourceGzTarPath := filepath.Join(absExtraSourceDirPath, sourceGzTarDirName)
		fileNames, err = utils.GetFileNamesFromDir(absSourceGzTarPath)
		if err != nil {
			return false, fmt.Errorf("error while getting files from %s: %v", sourceGzTarDirName, err)
		}
		if len(fileNames) == 0 {
			return false, fmt.Errorf("no file found under extra_src_dir/<repo-name>-<git-sha>")
		}
	}

	return true, nil
}

// NewGithubClient creates a GitHub client with custom organization.
// The token is retrieved in the same way as what SuiteController does.
func NewGithubClient(organization string) (*github.Github, error) {
	token := utils.GetEnv(constants.GITHUB_TOKEN_ENV, "")
	if gh, err := github.NewGithubClient(token, organization); err != nil {
		return nil, err
	} else {
		return gh, nil
	}
}

// ReadFileFromGitRepo reads a file from a remote Git repository hosted in GitHub.
// The filePath should be a relative path to the root of the repository.
// File content is returned. If error occurs, the error will be returned and
// empty string is returned as nothing is read.
// If branch is omitted, file is read from the "main" branch.
func ReadFileFromGitRepo(repoUrl, filePath, branch string) (string, error) {
	fromBranch := branch
	if fromBranch == "" {
		fromBranch = "main"
	}
	wrapErr := func(err error) error {
		return fmt.Errorf("error while reading file %s from repository %s: %v", filePath, repoUrl, err)
	}
	parsedUrl, err := url.Parse(repoUrl)
	if err != nil {
		return "", wrapErr(err)
	}
	org, repo := path.Split(parsedUrl.Path)
	gh, err := NewGithubClient(strings.Trim(org, "/"))
	if err != nil {
		return "", wrapErr(err)
	}
	repoContent, err := gh.GetFile(repo, filePath, fromBranch)
	if err != nil {
		return "", wrapErr(err)
	}
	if content, err := repoContent.GetContent(); err != nil {
		return "", wrapErr(err)
	} else {
		return content, nil
	}
}

// ReadRequirements reads dependencies from compiled requirements.txt by pip-compile,
// and it assumes the requirements.txt is simple in the root of the repository.
// The requirements are returned a list of strings, each of them is in form name==version.
func ReadRequirements(repoUrl string) ([]string, error) {
	const requirementsFile = "requirements.txt"

	wrapErr := func(err error) error {
		return fmt.Errorf("error while reading requirements.txt from repo %s: %v", repoUrl, err)
	}

	content, err := ReadFileFromGitRepo(repoUrl, requirementsFile, "")
	if err != nil {
		return nil, wrapErr(err)
	}

	reqs := make([]string, 0, 5)
	// Match line: "requests==2.31.0 \"
	reqRegex := regexp.MustCompile(`^\S.+ \\$`)

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if reqRegex.MatchString(line) {
			reqs = append(reqs, strings.TrimSuffix(line, " \\"))
		}
	}

	return reqs, nil
}

func IsPreFetchDependenciesFilesExists(gitUrl, absExtraSourceDirPath string, isHermetic bool, prefetchValue string) (bool, error) {
	var absDependencyPath string
	switch prefetchValue {
	case "gomod":
		fmt.Println("Checking go dependency files")
		absDependencyPath = filepath.Join(absExtraSourceDirPath, gomodDependencySubDir)
	case "pip":
		fmt.Println("Checking python dependency files")
		absDependencyPath = filepath.Join(absExtraSourceDirPath, pipDependencySubDir)
	default:
		return false, fmt.Errorf("pre-fetch value type is not implemented")
	}

	fileNames, err := utils.GetFileNamesFromDir(absDependencyPath)
	if err != nil {
		return false, fmt.Errorf("error while getting files from %s: %v", absDependencyPath, err)
	}
	if len(fileNames) == 0 {
		return false, fmt.Errorf("no file found under extra_src_dir/deps/")
	}

	// Easy to check for pip. Check if all requirements are included in the built source image.
	if prefetchValue == "pip" {
		fileSet := make(map[string]int)
		for _, name := range fileNames {
			fileSet[name] = 1
		}
		fmt.Println("file set:", fileSet)

		requirements, err := ReadRequirements(gitUrl)
		fmt.Println("requirements:", requirements)
		if err != nil {
			return false, fmt.Errorf("error while reading requirements.txt from repo %s: %v", gitUrl, err)
		}
		var sdistFilename string
		for _, requirement := range requirements {
			if strings.Contains(requirement, "==") {
				sdistFilename = strings.Replace(requirement, "==", "-", 1) + ".tar.gz"
			} else if strings.Contains(requirement, " @ https://") {
				sdistFilename = fmt.Sprintf("external-%s", strings.Split(requirement, " ")[0])
			} else {
				fmt.Println("unknown requirement form:", requirement)
				continue
			}
			if _, exists := fileSet[sdistFilename]; !exists {
				return false, fmt.Errorf("requirement '%s' is not included", requirement)
			}
		}
	}

	return true, nil
}

// readDockerfile reads Dockerfile dockerfile from repository repoURL.
// The Dockerfile is resolved by following the logic applied to the buildah task definition.
func readDockerfile(pathContext, dockerfile, repoURL, repoRevision string) ([]byte, error) {
	tempRepoDir, err := os.MkdirTemp("", "-test-repo")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempRepoDir)
	testRepo, err := git.PlainClone(tempRepoDir, false, &git.CloneOptions{URL: repoURL})
	if err != nil {
		return nil, err
	}

	// checkout to the revision. use go-git ResolveRevision since revision could be a branch, tag or commit hash
	commitHash, err := testRepo.ResolveRevision(plumbing.Revision(repoRevision))
	if err != nil {
		return nil, err
	}
	workTree, err := testRepo.Worktree()
	if err != nil {
		return nil, err
	}
	if err := workTree.Checkout(&git.CheckoutOptions{Hash: *commitHash}); err != nil {
		return nil, err
	}

	// check dockerfile in different paths
	var dockerfilePath string
	dockerfilePath = filepath.Join(tempRepoDir, dockerfile)
	if content, err := os.ReadFile(dockerfilePath); err == nil {
		return content, nil
	}
	dockerfilePath = filepath.Join(tempRepoDir, pathContext, dockerfile)
	if content, err := os.ReadFile(dockerfilePath); err == nil {
		return content, nil
	}
	if strings.HasPrefix(dockerfile, "https://") {
		if resp, err := http.Get(dockerfile); err == nil {
			defer resp.Body.Close()
			if body, err := io.ReadAll(resp.Body); err == nil {
				return body, err
			} else {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return nil, fmt.Errorf("resolveDockerfile: can't resolve Dockerfile from path context %s and dockerfile %s", pathContext, dockerfile)
}

// ReadDockerfileUsedForBuild reads the Dockerfile and return its content.
func ReadDockerfileUsedForBuild(c client.Client, tektonController *tekton.TektonController, pr *pipeline.PipelineRun) ([]byte, error) {
	var paramDockerfileValue, paramPathContextValue, paramUrlValue, paramRevisionValue string
	var err error

	for _, param := range pr.Spec.Params {
		switch param.Name {
		case "dockerfile":
			paramDockerfileValue = param.Value.StringVal
		case "path-context":
			paramPathContextValue = param.Value.StringVal
		case "git-url":
			paramUrlValue = param.Value.StringVal
		case "revision":
			paramRevisionValue = param.Value.StringVal
		}
	}

	dockerfileContent, err := readDockerfile(paramPathContextValue, paramDockerfileValue, paramUrlValue, paramRevisionValue)
	if err != nil {
		return nil, err
	}
	return dockerfileContent, nil
}

type SourceBuildResult struct {
	Status                  string `json:"status"`
	Message                 string `json:"message,omitempty"`
	DependenciesIncluded    bool   `json:"dependencies_included"`
	BaseImageSourceIncluded bool   `json:"base_image_source_included"`
	ImageUrl                string `json:"image_url"`
	ImageDigest             string `json:"image_digest"`
}

// ReadSourceBuildResult reads source-build task result BUILD_RESULT and returns the decoded data.
func ReadSourceBuildResult(c client.Client, tektonController *tekton.TektonController, pr *pipeline.PipelineRun) (*SourceBuildResult, error) {
	sourceBuildResult, err := tektonController.GetTaskRunResult(c, pr, "build-source-image", "BUILD_RESULT")
	if err != nil {
		return nil, err
	}
	var buildResult SourceBuildResult
	if err = json.Unmarshal([]byte(sourceBuildResult), &buildResult); err != nil {
		return nil, err
	}
	return &buildResult, nil
}

type Dockerfile struct {
	parsedContent *parser.Result
}

func ParseDockerfile(content []byte) (*Dockerfile, error) {
	parsedContent, err := parser.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	df := Dockerfile{
		parsedContent: parsedContent,
	}
	return &df, nil
}

func (d *Dockerfile) ParentImages() []string {
	parentImages := make([]string, 0, 5)
	for _, child := range d.parsedContent.AST.Children {
		if child.Value == "FROM" {
			parentImages = append(parentImages, child.Next.Value)
		}
	}
	return parentImages
}

func (d *Dockerfile) IsBuildFromScratch() bool {
	parentImages := d.ParentImages()
	return parentImages[len(parentImages)-1] == "scratch"
}

// convertImageToBuildahOutputForm converts an image pullspec to a
// format corresponding to `buildah images --format '{{ .Name }}:{{ .Tag }}@{{ .Digest }}'`
func convertImageToBuildahOutputForm(imagePullspec string) (string, error) {
	ref, err := reference.Parse(imagePullspec)
	if err != nil {
		return "", fmt.Errorf("fail to parse image %s: %s", imagePullspec, err)
	}
	var tag string
	digest := ref.ID
	if digest == "" {
		val, err := FetchImageDigest(imagePullspec)
		if err != nil {
			return "", fmt.Errorf("fail to fetch image digest of %s: %s", imagePullspec, err)
		}
		digest = val

		tag = ref.Tag
		if tag == "" {
			tag = "latest"
		}
	} else {
		tag = "<none>"
	}
	digest = strings.TrimPrefix(digest, "sha256:")
	// image could have no namespace.
	converted := strings.TrimSuffix(filepath.Join(ref.Registry, ref.Namespace), "/")
	return fmt.Sprintf("%s/%s:%s@sha256:%s", converted, ref.Name, tag, digest), nil
}

// ConvertParentImagesToBuildahOutputForm converts the image pullspecs found in the Dockerfile
// to a format corresponding to `buildah images --format '{{ .Name }}:{{ .Tag }}@{{ .Digest }}'`.
// ConvertParentImagesToBuildahOutputForm de-duplicates the images.
func (d *Dockerfile) ConvertParentImagesToBuildahOutputForm() ([]string, error) {
	convertedImagePullspecs := make([]string, 0, 5)
	seen := make(map[string]int)
	parentImages := d.ParentImages()
	for _, imagePullspec := range parentImages {
		if imagePullspec == "scratch" {
			continue
		}
		if strings.HasPrefix(imagePullspec, "oci-archive:") {
			continue
		}
		if _, exists := seen[imagePullspec]; exists {
			continue
		}
		seen[imagePullspec] = 1
		if converted, err := convertImageToBuildahOutputForm(imagePullspec); err == nil {
			convertedImagePullspecs = append(convertedImagePullspecs, converted)
		} else {
			return nil, err
		}
	}
	return convertedImagePullspecs, nil
}

func isRegistryAllowed(registry string) bool {
	// For the list of allowed registries, refer to source-build task definition.
	allowedRegistries := map[string]int{
		"registry.access.redhat.com": 1,
		"registry.redhat.io":         1,
	}
	_, exists := allowedRegistries[registry]
	return exists
}

func IsImagePulledFromAllowedRegistry(imagePullspec string) (bool, error) {
	if ref, err := reference.Parse(imagePullspec); err == nil {
		return isRegistryAllowed(ref.Registry), nil
	} else {
		return false, err
	}
}

func SourceBuildTaskRunLogsContain(
	tektonController *tekton.TektonController, pr *pipeline.PipelineRun, message string) (bool, error) {
	logs, err := tektonController.GetTaskRunLogs(pr.GetName(), "build-source-image", pr.GetNamespace())
	if err != nil {
		return false, err
	}
	for _, logMessage := range logs {
		if strings.Contains(logMessage, message) {
			return true, nil
		}
	}
	return false, nil
}

func ResolveSourceImageByVersionRelease(image string) (string, error) {
	config, err := FetchImageConfig(image)
	if err != nil {
		return "", err
	}
	labels := config.Config.Labels
	var version, release string
	var exists bool
	if version, exists = labels["version"]; !exists {
		return "", fmt.Errorf("cannot find out version label from image config")
	}
	if release, exists = labels["release"]; !exists {
		return "", fmt.Errorf("cannot find out release label from image config")
	}
	ref, err := reference.Parse(image)
	if err != nil {
		return "", err
	}
	ref.ID = ""
	ref.Tag = fmt.Sprintf("%s-%s-source", version, release)
	return ref.Exact(), nil
}

func AllParentSourcesIncluded(parentSourceImage, builtSourceImage string) (bool, error) {
	parentConfig, err := FetchImageConfig(parentSourceImage)
	if err != nil {
		return false, fmt.Errorf("error while getting parent source image manifest %s: %w", parentSourceImage, err)
	}
	builtConfig, err := FetchImageConfig(builtSourceImage)
	if err != nil {
		return false, fmt.Errorf("error while getting built source image manifest %s: %w", builtSourceImage, err)
	}
	srpmSha256Sums := make(map[string]int)
	var parts []string
	for _, history := range builtConfig.History {
		// Example history: #(nop) bsi version 0.2.0-dev adding artifact: 5f526f4
		parts = strings.Split(history.CreatedBy, " ")
		// The last part 5f526f4 is the checksum calculated from the file included in the generated blob.
		srpmSha256Sums[parts[len(parts)-1]] = 1
	}
	for _, history := range parentConfig.History {
		parts = strings.Split(history.CreatedBy, " ")
		if _, exists := srpmSha256Sums[parts[len(parts)-1]]; !exists {
			return false, nil
		}
	}
	return true, nil
}

func ResolveKonfluxSourceImage(image string) (string, error) {
	digest, err := FetchImageDigest(image)
	if err != nil {
		return "", fmt.Errorf("error while fetching image digest of %s: %w", image, err)
	}
	ref, err := reference.Parse(image)
	if err != nil {
		return "", fmt.Errorf("error while parsing image %s: %w", image, err)
	}
	ref.ID = ""
	ref.Tag = fmt.Sprintf("sha256-%s.src", digest)
	return ref.Exact(), nil
}
