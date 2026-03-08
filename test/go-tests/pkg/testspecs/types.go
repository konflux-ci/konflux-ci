package testspecs

import (
	"fmt"
	"strings"
)

/*
I've modeled this based on Ginkgo's
model used by the outline command but it is much
simpler and tailored to our BDD
Test Spec and Test Outline concepts
*/
type TestSpecNode struct {
	Name                 string
	Text                 string
	Labels               []string
	Nodes                TestOutline
	InnerParentContainer bool
	LineSpaceLevel       int
}

type TestOutline []TestSpecNode

type TemplateData struct {
	Outline                 TestOutline
	PackageName             string
	FrameworkDescribeString string
}

type Translator interface {
	FromFile(file string) (TestOutline, error)
	ToFile(destination string, outline TestOutline) error
}

func (to *TestOutline) ToString() string {

	return recursiveNodeStringBuilder(*to, 0)
}

func recursiveNodeStringBuilder(nodes TestOutline, printWidth int) string {

	var b strings.Builder
	defer b.Reset()
	for _, n := range nodes {
		var labels string
		var annotate []string
		if len(n.Labels) != 0 || n.Labels != nil {
			for _, n := range n.Labels {
				annotate = append(annotate, fmt.Sprintf("@%s", n))
			}
			labels = strings.Join(annotate, ", ")
		}
		if labels != "" {

			nodeString := fmt.Sprintf("\n%*s%s: %+v %+v", printWidth, "", n.Name, n.Text, labels)
			b.WriteString(nodeString)
		} else {
			b.WriteString(fmt.Sprintf("\n%*s%s: %+v", printWidth, "", n.Name, n.Text))
		}

		if len(n.Nodes) != 0 {
			printWidth += 2
			b.WriteString(recursiveNodeStringBuilder(n.Nodes, printWidth))
			printWidth -= 2
		}

		if n.InnerParentContainer && (n.Name != "It" && n.Name != "By") {
			b.WriteString("\n")
		}

	}
	return b.String()

}
