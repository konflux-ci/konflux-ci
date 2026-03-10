package logs

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	"github.com/onsi/ginkgo/v2"
	types "github.com/onsi/ginkgo/v2/types"
	"sigs.k8s.io/yaml"
)

func GetClassnameFromReport(report types.SpecReport) string {
	texts := []string{}
	texts = append(texts, report.ContainerHierarchyTexts...)
	if report.LeafNodeText != "" {
		texts = append(texts, report.LeafNodeText)
	}
	if len(texts) > 0 {
		classStrings := strings.Fields(texts[0])
		return classStrings[0][1:]
	}
	return strings.Join(texts, " ")
}

func ShortenStringAddHash(report types.SpecReport) string {
	className := GetClassnameFromReport(report)
	s := report.FullText()
	replacedClass := strings.Replace(s, className, "", 1)
	if len(replacedClass) > 100 {
		h := sha1.New()
		h.Write([]byte(replacedClass[100:]))
		return replacedClass[0:100] + " sha: " + hex.EncodeToString(h.Sum(nil))
	}
	return replacedClass
}

func createArtifactDirectory() (string, error) {
	wd, _ := os.Getwd()
	artifactDir := utils.GetEnv("ARTIFACT_DIR", fmt.Sprintf("%s/tmp", wd))
	classname := ShortenStringAddHash(ginkgo.CurrentSpecReport())
	testLogsDir := fmt.Sprintf("%s/%s", artifactDir, classname)
	if err := os.MkdirAll(testLogsDir, os.ModePerm); err != nil {
		return "", err
	}
	return testLogsDir, nil
}

func StoreResourceYaml(resource any, name string) error {
	resourceYaml, err := yaml.Marshal(resource)
	if err != nil {
		return fmt.Errorf("error getting resource yaml: %v", err)
	}
	return StoreArtifacts(map[string][]byte{name + ".yaml": resourceYaml})
}

func StoreArtifacts(artifacts map[string][]byte) error {
	dir, err := createArtifactDirectory()
	if err != nil {
		return err
	}
	for name, data := range artifacts {
		if err := os.WriteFile(fmt.Sprintf("%s/%s", dir, name), data, 0644); err != nil {
			return err
		}
	}
	return nil
}

func StoreTestTiming() error {
	dir, err := createArtifactDirectory()
	if err != nil {
		return err
	}
	testTime := "Test started at: " + ginkgo.CurrentSpecReport().StartTime.String() + "\nTest ended at: " + time.Now().String()
	return os.WriteFile(fmt.Sprintf("%s/test-timing", dir), []byte(testTime), 0644)
}
