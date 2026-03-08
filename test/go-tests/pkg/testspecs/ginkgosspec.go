package testspecs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/magefile/mage/sh"
	"k8s.io/klog/v2"
)

type GinkgosSpecTranslator struct {
}

var ginkgoOutlineJsonCmd = sh.OutCmd("ginkgo", "outline", "--format", "json")
var ginkgoGenerateSpecCmd = sh.OutCmd("ginkgo", "generate")

// New returns a Ginkgo Spec Translator
func NewGinkgoSpecTranslator() *GinkgosSpecTranslator {

	return &GinkgosSpecTranslator{}
}

// FromFile generates a TestOutline from a Ginkgo test File
func (gst *GinkgosSpecTranslator) FromFile(file string) (TestOutline, error) {

	var nodes TestOutline
	output, err := ginkgoOutlineJsonCmd(file)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal([]byte(output), &nodes)
	if err != nil {
		klog.Error("Failed to unmarshal spec outline")
		return nil, err
	}
	markInnerParentContainer(nodes)
	nodes = excludeSetupTeardownNodes(nodes)
	frameworkDescribeNode, err := ExtractFrameworkDescribeNode(file)
	if err != nil {
		klog.Error("Failed to extract the framework describe node from the AST")
		return nil, err
	}
	if reflect.ValueOf(frameworkDescribeNode).IsZero() {
		// we assume its a normal Ginkgo Spec file so return it
		return nodes, nil
	}
	frameworkDescribeNode.Nodes = nodes
	return TestOutline{frameworkDescribeNode}, nil
}

// ToFile generates a Ginkgo test file from a TestOutline
func (gst *GinkgosSpecTranslator) ToFile(destination, teamTmplPath string, outline TestOutline) error {

	e2ePath, err := os.Getwd()
	if err != nil {
		klog.Error("failed to get current directory")
		return err
	}

	testFilePath, err := createTestPath(e2ePath, destination)
	if err != nil {
		return err
	}
	dataFile, err := writeTemplateDataFile(e2ePath, testFilePath, outline)
	if err != nil {
		return err
	}

	return generateGinkgoSpec(e2ePath, teamTmplPath, testFilePath, dataFile)

}

// markInnerParentContainer marks whether
// node is a parent container that comes after
// the first root node which is the framework
// describe decorator function
func markInnerParentContainer(nodes TestOutline) {

	for i := range nodes {
		nodes[i].InnerParentContainer = true
	}
}

// excludeSetupTeardownNodes removes those nodes from the ginkgo
// outline output since they don't included anything useful anyways
func excludeSetupTeardownNodes(nodes TestOutline) TestOutline {
	excludes := []string{"JustBeforeEach", "BeforeEach", "BeforeAll", "JustAfterEach", "AfterAll", "AfterEach"}
	for i := 0; i < len(nodes); i++ {
		for _, ex := range excludes {
			if ex == nodes[i].Name {
				nodes = append(nodes[:i], nodes[i+1:]...)
				nodes = excludeSetupTeardownNodes(nodes)
				break
			}

		}

		if len(nodes[i].Nodes) != 0 {
			nodes[i].Nodes = excludeSetupTeardownNodes(nodes[i].Nodes)
		}
	}

	return nodes

}

// generateGinkgoSpec will call the ginkgo generate command
// to generate the ginkgo data json file we created and
// the template located in out templates directory
func generateGinkgoSpec(cwd, teamTmplPath, destination string, dataFile string) error {

	var err error

	if teamTmplPath != TestFilePath {
		tmplFile, err := mergeTemplates(teamTmplPath, SpecsPath)
		if err != nil {
			return err
		}
		defer os.Remove(tmplFile.Name())
		teamTmplPath = tmplFile.Name()
	}

	// Note I change into the directory and rename things because ginkgo
	// by default generates the test file name as <package>_test.go.
	// Since that is not a semantic we follow I perform this action.
	dirs := strings.Split(filepath.Dir(destination), "/")
	dir := dirs[len(dirs)-1]
	dir = strings.ReplaceAll(dir, "-", "_")
	ginkgoFileName := fmt.Sprintf("%s_test.go", dir)
	postFileName := filepath.Base(destination)
	err = os.Chdir(filepath.Dir(destination))
	if err != nil {
		err = os.Remove(dataFile)
		if err != nil {
			return err
		}
		return err
	}
	// Doing this to avoid errcheck flagging this in a defer.
	// Refer to https://github.com/kisielk/errcheck
	// issues 101, 77, 55

	klog.Infof("Creating new test package directory and spec file %s.\n", destination)
	_, err = ginkgoGenerateSpecCmd("--template", teamTmplPath, "--template-data", dataFile)
	if err != nil {
		err = os.Remove(ginkgoFileName)
		if err != nil {
			return err
		}
		err = os.Remove(dataFile)
		if err != nil {
			return err
		}
		return err
	}
	err = os.Rename(ginkgoFileName, postFileName)
	if err != nil {
		return err
	}

	err = os.Remove(dataFile)
	if err != nil {
		return err
	}
	err = os.Chdir(cwd)
	if err != nil {
		return err
	}
	return err
}

// mergeTemplates creates a new template file from files provided in the argument
func mergeTemplates(paths ...string) (*os.File, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	tempFile, err := os.CreateTemp(cwd, "merged-tmpl")
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	for _, templatePath := range paths {
		// Avoid possible memory leak caused by defer by wrapping in a function
		appendToTempPath := func() error {
			tmplFile, err := os.Open(path.Clean(templatePath))
			if err != nil {
				return err
			}
			defer tmplFile.Close()

			_, err = io.Copy(tempFile, tmplFile)
			if err != nil {
				return err
			}

			_, err = tempFile.Write([]byte{'\n', '\n'})
			if err != nil {
				return err
			}
			return nil
		}
		err = appendToTempPath()
		if err != nil {
			return nil, fmt.Errorf("error during appending to temp templatePath: %+v", err)
		}
	}
	return tempFile, nil
}

// createTestPath will create the full test path in the tests
// directory if it doesn't exit
func createTestPath(cwd string, destination string) (string, error) {

	destination, err := filepath.Abs(destination)
	if err != nil {
		klog.Error("failed to get absolute path of destination")
		return "", err
	}

	testPath := filepath.Join(cwd, "tests")
	if !strings.Contains(destination, testPath) {

		return "", fmt.Errorf("the destination path must be to the `e2e-tests/tests` directory")
	}

	testDir, _ := filepath.Split(destination)
	dirs := strings.Split(testDir, "/")
	// remove whitespaces trailing (/) from filepath split
	length := len(dirs)
	dirs = dirs[:length-1]

	if strings.Contains(dirs[len(dirs)-1], "tests") {

		return "", fmt.Errorf("the destination path must be to `e2e-tests/tests/<sub-path>` directory")
	}

	dir := filepath.Dir(destination)
	err = os.MkdirAll(dir, 0775)
	if err != nil {
		klog.Errorf("failed to create package directory, %s", dir)
		return "", err
	}
	return destination, nil
}

// writeTemplateDataFile out the data as a json file to the directory that will be used by
// ginkgo generate command
func writeTemplateDataFile(cwd string, destination string, outline TestOutline) (string, error) {

	tmplData := NewTemplateData(outline, destination)
	data, err := json.Marshal(tmplData)
	if err != nil {
		klog.Errorf("error marshalling template data to json: %s", err)
		return "", err
	}
	dataName := strings.Split(filepath.Base(destination), ".")[0]
	dataFile := fmt.Sprintf("%s.json", dataName)
	err = os.Chdir(filepath.Dir(destination))
	if err != nil {
		return "", err
	}
	err = os.WriteFile(dataFile, data, 0644)
	if err != nil {
		return "", err
	}
	// Doing this to avoid errcheck flagging this in a defer.
	// Refer to https://github.com/kisielk/errcheck
	err = os.Chdir(cwd)
	if err != nil {
		return "", err
	}

	return dataFile, nil
}
