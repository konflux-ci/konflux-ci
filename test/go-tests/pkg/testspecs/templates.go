package testspecs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	sprig "github.com/go-task/slim-sprig"
	"github.com/magefile/mage/sh"
	"k8s.io/klog/v2"
)

func NewTemplateData(specOutline TestOutline, destination string) *TemplateData {

	// This regex will find all the words that start with capital letter
	// followed by lower case in the string
	name := specOutline[0].Name
	re := regexp.MustCompile(`(?:[A-Z][a-z]+[\s-]*)`)
	parts := re.FindAllString(name, -1)

	// Lets find the index of the first regex match. If it doesn't start
	// at index 0 then assume it starts with a capitalized acronym
	// i.e. JVM, etc. and append it to the regex list at index 0
	nidx := strings.Index(name, parts[0])
	if nidx != 0 {
		n := strings.ToLower(name[:nidx])
		parts = append(parts[:1], parts[0:]...)
		parts[0] = n[:nidx]
	}

	for i, word := range parts {
		parts[i] = strings.ToLower(word)
	}
	newSpecName := strings.Join(parts[:len(parts)-1], "-")

	dir := filepath.Dir(destination)
	dirName := strings.Split(dir, "/")[len(strings.Split(dir, "/"))-1]
	packageName := regexp.MustCompile(`^([a-z]+)`).FindString(dirName)

	return &TemplateData{Outline: specOutline, PackageName: packageName, FrameworkDescribeString: newSpecName}
}

func RenderFrameworkDescribeGoFile(t TemplateData) error {
	var describeFile = "pkg/framework/describe.go"

	err := renderTemplate(describeFile, FrameworkDescribePath, t, true)
	if err != nil {
		klog.Errorf("failed to append to pkg/framework/describe.go with : %s", err)
		return err
	}
	err = goFmt(describeFile)

	if err != nil {

		klog.Errorf("%s", err)
		return err
	}

	return nil

}

func goFmt(path string) error {
	err := sh.RunV("go", "fmt", path)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("Could not fmt:\n%s\n", path), err)
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func renderTemplate(destination, templatePath string, templateData interface{}, appendDestination bool) error {

	var templateText string
	var f *os.File
	var err error

	/* This decision logic feels a little clunky cause initially I wanted to
	to have this func create the new file and render the template into the new
	file. But with the updating the pkg/framework/describe.go use case
	I wanted to reuse leveraging the txt/template package rather than
	rendering/updating using strings/regex.
	*/
	if appendDestination {

		f, err = os.OpenFile(destination, os.O_APPEND|os.O_WRONLY, 0664)
		if err != nil {
			klog.Infof("Failed to open file: %v", err)
			return err
		}
	} else {

		if fileExists(destination) {
			return fmt.Errorf("%s already exists", destination)
		}
		f, err = os.Create(destination)
		if err != nil {
			klog.Infof("Failed to create file: %v", err)
			return err
		}
	}

	defer f.Close()

	tpl, err := os.ReadFile(templatePath)
	if err != nil {
		klog.Infof("error reading file: %v", err)
		return err

	}
	var tmplText = string(tpl)
	templateText = fmt.Sprintf("\n%s", tmplText)
	specTemplate, err := template.New("spec").Funcs(sprig.TxtFuncMap()).Parse(templateText)
	if err != nil {
		klog.Infof("error parsing template file: %v", err)
		return err

	}

	err = specTemplate.Execute(f, templateData)
	if err != nil {
		klog.Infof("error rendering template file: %v", err)
		return err
	}

	return nil
}
