package testspecs

import (
	"fmt"
	"os"
	"testing"
)

func TestMergeTemplates(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
		return
	}

	tempDir, err := os.MkdirTemp(cwd, "test-merge-templates")
	if err != nil {
		t.Fatal(err)
		return
	}
	defer os.RemoveAll(tempDir)

	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatal(err)
		return
	}

	var fileNames []string
	var expectedString string

	for i := 0; i < 10; i++ {
		file, err := os.CreateTemp(tempDir, "tempFile")
		if err != nil {
			t.Fatal(err)
			return
		}
		lineContent := fmt.Sprintf("This should be line number '%d' from '%s'", i, file.Name())
		_, err = file.WriteString(lineContent)
		if err != nil {
			t.Fatal(err)
			return
		}
		expectedString += lineContent + "\n\n"
		fileNames = append(fileNames, file.Name())
	}

	mergedFile, err := mergeTemplates(fileNames...)
	if err != nil {
		t.Errorf("failed to merge templates: %+v", err)
		return
	}

	mergedFile, err = os.Open(mergedFile.Name())
	if err != nil {
		t.Error(err)
		return
	}
	mergedFileBytes, err := os.ReadFile(mergedFile.Name())
	if err != nil {
		t.Error(err)
		return
	}

	mergedFileContent := string(mergedFileBytes)
	if mergedFileContent != expectedString {
		t.Errorf("content of merged file does not match the expected content")
	}
}
