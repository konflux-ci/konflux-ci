package utils

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	dockerconfig "github.com/docker/cli/cli/config"
	dockerconfigfile "github.com/docker/cli/cli/config/configfile"
	dockerconfigtypes "github.com/docker/cli/cli/config/types"

	"github.com/devfile/library/v2/pkg/util"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/mitchellh/go-homedir"
	"k8s.io/klog/v2"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type Options struct {
	ApiUrl string
	Token  string
}

// check options are valid or not
func CheckOptions(optionsArr []Options) (bool, error) {
	if len(optionsArr) == 0 {
		return false, nil
	}

	if len(optionsArr) > 1 {
		return true, fmt.Errorf("options array contains more than 1 object")
	}

	options := optionsArr[0]

	if options.ApiUrl == "" {
		return true, fmt.Errorf("apiUrl field is empty")
	}

	if options.Token == "" {
		return true, fmt.Errorf("token field is empty")
	}

	return true, nil
}

// CheckIfEnvironmentExists return true/false if the environment variable exists
func CheckIfEnvironmentExists(env string) bool {
	_, exist := os.LookupEnv(env)
	return exist
}

// Retrieve an environment variable. If will not exists will be used a default value
func GetEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// Retrieve an environment variable. If it doesn't exist, returns result of a call to `defaultFunc`.
func GetEnvOrFunc(key string, defaultFunc func() (string, error)) (string, error) {
	if val := os.Getenv(key); val != "" {
		return val, nil
	}
	return defaultFunc()
}

// GetReleaseServiceCatalogRevision returns RELEASE_SERVICE_CATALOG_REVISION from env, or from
// test/e2e/release-service-catalog-revision (env-var file with one variable), or "development".
func GetReleaseServiceCatalogRevision() string {
	if v := os.Getenv("RELEASE_SERVICE_CATALOG_REVISION"); v != "" {
		return v
	}
	for _, path := range []string{"../e2e/release-service-catalog-revision", "test/e2e/release-service-catalog-revision"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "RELEASE_SERVICE_CATALOG_REVISION=") {
				v := strings.TrimPrefix(line, "RELEASE_SERVICE_CATALOG_REVISION=")
				v = strings.Trim(v, "\"'")
				if v != "" {
					return v
				}
			}
		}
	}
	return "development"
}

func GetQuayIOOrganization() string {
	return GetEnv(constants.QUAY_E2E_ORGANIZATION_ENV, "redhat-appstudio-qe")
}

func IsPrivateHostname(url string) bool {
	// https://www.ibm.com/docs/en/networkmanager/4.2.0?topic=translation-private-address-ranges
	privateIPAddressPrefixes := []string{"10.", "172.1", "172.2", "172.3", "192.168"}
	addr, err := net.LookupIP(url)
	if err != nil {
		klog.Infof("Unknown host: %v", err)
		return true
	}

	ip := addr[0]
	for _, ipPrefix := range privateIPAddressPrefixes {
		if strings.HasPrefix(ip.String(), ipPrefix) {
			return true
		}
	}
	return false
}

func GetOpenshiftToken() (token string, err error) {
	// Get the token for the current openshift user
	tokenBytes, err := exec.Command("oc", "whoami", "--show-token").Output()
	if err != nil {
		return "", fmt.Errorf("error obtaining oc token %s", err.Error())
	}
	return strings.TrimSuffix(string(tokenBytes), "\n"), nil
}

func GetGeneratedNamespace(name string) string {
	return name + "-" + util.GenerateRandomString(4)
}

func WaitUntilWithInterval(cond wait.ConditionFunc, interval time.Duration, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), interval, timeout, true, func(ctx context.Context) (bool, error) { return cond() })
}

func WaitUntil(cond wait.ConditionFunc, timeout time.Duration) error {
	return WaitUntilWithInterval(cond, time.Second, timeout)
}

func ExecuteCommandInASpecificDirectory(command string, args []string, directory string) error {
	cmd := exec.Command(command, args...) // nolint:gosec
	cmd.Dir = directory

	stdin, err := cmd.StdinPipe()

	if err != nil {
		return err
	}
	defer stdin.Close() // the doc says subProcess.Wait will close it, but I'm not sure, so I kept this line

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err = cmd.Start(); err != nil {
		klog.Errorf("an error occurred: %s", err)

		return err
	}

	_, _ = io.WriteString(stdin, "4\n")

	if err := cmd.Wait(); err != nil {
		return err
	}

	return err
}

func ToPrettyJSONString(v interface{}) string {
	s, _ := json.MarshalIndent(v, "", "  ")
	return string(s)
}

// GetAdditionalInfo adds information regarding the application name and namespace of the test
func GetAdditionalInfo(applicationName, namespace string) string {
	return fmt.Sprintf("(application: %s, namespace: %s)", applicationName, namespace)
}

// contains checks if a string is present in a slice
func Contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func MergeMaps(m1, m2 map[string]string) map[string]string {
	resultMap := make(map[string]string)
	for k, v := range m1 {
		resultMap[k] = v
	}
	for k, v := range m2 {
		resultMap[k] = v
	}
	return resultMap
}

// CreateDockerConfigFile takes base64 encoded dockerconfig.json and saves it locally (/<home-directory/.docker/config.json)
func CreateDockerConfigFile(base64EncodedString string) error {
	var rawRegistryCreds []byte
	var homeDir string
	var err error

	if rawRegistryCreds, err = base64.StdEncoding.DecodeString(base64EncodedString); err != nil {
		return fmt.Errorf("unable to decode container registry credentials: %v", err)
	}
	if homeDir, err = homedir.Dir(); err != nil {
		return fmt.Errorf("unable to locate home directory: %v", err)
	}
	if err = os.MkdirAll(homeDir+"/.docker", 0775); err != nil {
		return fmt.Errorf("failed to create '.docker' config directory: %v", err)
	}
	if err = os.WriteFile(homeDir+"/.docker/config.json", rawRegistryCreds, 0644); err != nil {
		return fmt.Errorf("failed to create a docker config file: %v", err)
	}

	return nil
}

// Return a container logs from a given pod and namespace
func GetContainerLogs(ki kubernetes.Interface, podName, containerName, namespace string) (string, error) {
	podLogOpts := corev1.PodLogOptions{
		Container: containerName,
	}

	req := ki.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(context.Background())
	if err != nil {
		return "", fmt.Errorf("error in opening the stream: %v", err)
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", fmt.Errorf("error in copying logs to buf, %v", err)
	}
	return buf.String(), nil
}

func ExtractGitRepositoryNameFromURL(url string) (name string) {
	repoName := url[strings.LastIndex(url, "/")+1:]
	return strings.TrimSuffix(repoName, ".git")
}

func GetGithubAppID() (int64, error) {
	appIDStr := GetEnv("E2E_PAC_GITHUB_APP_ID", constants.DefaultPaCGitHubAppID)

	id, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Build a kubeconfig string from an existing client config
func CreateKubeconfigFileForRestConfig(restConfig rest.Config) ([]byte, error) {
	clusters := make(map[string]*clientcmdapi.Cluster)
	clusters["default-cluster"] = &clientcmdapi.Cluster{
		Server:                   restConfig.Host,
		CertificateAuthorityData: restConfig.CAData,
		InsecureSkipTLSVerify:    true,
	}
	contexts := make(map[string]*clientcmdapi.Context)
	contexts["default-context"] = &clientcmdapi.Context{
		Cluster:  "default-cluster",
		AuthInfo: "default-user",
	}
	authinfos := make(map[string]*clientcmdapi.AuthInfo)
	authinfos["default-user"] = &clientcmdapi.AuthInfo{
		Token: string(restConfig.BearerToken),
	}
	clientConfig := clientcmdapi.Config{
		Kind:           "Config",
		APIVersion:     "v1",
		Clusters:       clusters,
		Contexts:       contexts,
		CurrentContext: "default-context",
		AuthInfos:      authinfos,
	}
	kubeconfiString, err := clientcmd.Write(clientConfig)
	if err != nil {
		return []byte{}, nil
	}
	return kubeconfiString, nil
}

func GetFileNamesFromDir(dirPath string) ([]string, error) {
	var filesInDir []string
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("error reading directory: %v", err)
	}
	for _, file := range files {
		filesInDir = append(filesInDir, file.Name())
	}
	return filesInDir, nil
}

func CheckFileExistsInDir(rootDir, filename string) (bool, error) {
	absFilePath := filepath.Join(rootDir, filename)
	_, err := os.Stat(absFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func Untar(dst string, tarPath string) error {
	tr, err := ReadTarFile(tarPath)
	if err != nil {
		return err
	}

	for {
		header, err := tr.Next()

		switch {

		// if no more files are found return
		case err == io.EOF:
			return nil

		// return any other error
		case err != nil:
			return err

		// if the header is nil, just skip it (not sure how this happens)
		case header == nil:
			continue
		}

		// the target location where the dir/file should be created
		target := filepath.Join(dst, header.Name) // nolint:gosec

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			err := CreateDir(target)
			if err != nil {
				return err
			}
		// if it's a file create it
		case tar.TypeReg:
			dirPath := filepath.Dir(target)
			if err := CreateDir(dirPath); err != nil {
				return fmt.Errorf("error when create parent directories %s: %v", dirPath, err)
			}
			err = CreateFile(target, header, tr)
			if err != nil {
				return err
			}
		}
	}
}

func ReadTarFile(tarPath string) (*tar.Reader, error) {
	tarFile, err := os.Open(tarPath) // nolint:gosec
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(tarFile)
	if strings.HasSuffix(tarPath, ".gz") || strings.HasSuffix(tarPath, ".gzip") {
		gz, err := gzip.NewReader(tarFile)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		tr = tar.NewReader(gz)
	}
	return tr, nil
}

func CreateDir(target string) error {
	if _, err := os.Stat(target); err != nil {
		if err := os.MkdirAll(target, 0755); err != nil { // nolint:gosec
			return err
		}
	}
	return nil
}

func CreateFile(target string, header *tar.Header, tr *tar.Reader) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode)) // nolint:gosec
	if err != nil {
		return err
	}

	// copy over contents
	if _, err := io.Copy(f, tr); err != nil { // nolint:gosec
		return err
	}

	// manually close here after each file operation; defering would cause each file close
	// to wait until all operations have completed.
	return f.Close()
}

func GetRepoName(repoUrl string) string {
	return strings.Split(strings.TrimSuffix(repoUrl, ".git"), "/")[4]
}

func FilterSliceUsingPattern(pattern string, lString []string) []string {
	var results []string
	re := regexp.MustCompile(pattern)
	for _, str := range lString {
		if re.MatchString(str) {
			results = append(results, str)
		}
	}
	return results
}

// getAuthForImageRef searches a given Docker configuration file for authentication credentials
// that match the provided image reference. It attempts to find the most specific match first
// (e.g., full repository name), then falls back to less specific matches (e.g., namespace,
// or just the registry host). It returns the first suitable AuthConfig found.
func getAuthForImageRef(cfg *dockerconfigfile.ConfigFile, imageRef name.Reference) (dockerconfigtypes.AuthConfig, error) {
	// Extract namespace from image reference repository
	// i.e. namespace/repository -> namespace
	var namespace string
	parts := strings.Split(imageRef.Context().RepositoryStr(), "/")
	if len(parts) > 1 {
		namespace = parts[0]
	}

	matchingEntries := []string{
		// quay.io/namespace/repo
		imageRef.Context().Name(),
		// quay.io/namespace
		fmt.Sprintf("%s/%s", imageRef.Context().RegistryStr(), namespace),
		// quay.io
		imageRef.Context().RegistryStr(),
	}

	for _, entry := range matchingEntries {
		authConfig, err := cfg.GetAuthConfig(entry)
		if err != nil {
			return authConfig, fmt.Errorf("failed to get auth config for %s: %+v", entry, err)
		}
		if authConfig.ServerAddress == entry && authConfig.Username != "" {
			return authConfig, nil
		}
	}

	return dockerconfigtypes.AuthConfig{}, fmt.Errorf("no suitable auth config matches image ref %s", imageRef.String())
}

// GetAuthenticatorForImageRef decodes a base64-encoded Docker configuration JSON string,
// loads it into a Docker config object, and then uses it to find appropriate
// authentication credentials for the given image reference. If suitable credentials
// (with a username) are found, it returns an `authn.Authenticator` instance
// that can be used for authenticating with container registries.
func GetAuthenticatorForImageRef(imageRef name.Reference, encodedDockerconfigjson string) (authenticator authn.Authenticator, err error) {
	var rawRegistryCreds []byte
	if rawRegistryCreds, err = base64.StdEncoding.DecodeString(encodedDockerconfigjson); err != nil {
		return authenticator, fmt.Errorf("unable to decode container registry credentials: %v", err)
	}
	dockerConfig, err := dockerconfig.LoadFromReader(strings.NewReader(string(rawRegistryCreds)))
	if err != nil {
		return authenticator, fmt.Errorf("failed to load docker config from supplied dockerconfigjson: %+v", err)
	}
	// Resolve credentials for a specified image reference
	authConfig, err := getAuthForImageRef(dockerConfig, imageRef)
	if err != nil {
		return authenticator, fmt.Errorf("failed to get auth for image ref: %+v", err)
	}

	if authConfig.Username != "" {
		klog.Infof("found credentials for image ref %s -> user: %s\n", imageRef.String(), authConfig.Username)
		return authn.FromConfig(authn.AuthConfig{
			Username: authConfig.Username,
			Password: authConfig.Password,
		}), nil
	}
	return authenticator, fmt.Errorf("did not find any suitable credentials for image ref %s", imageRef.String())
}
