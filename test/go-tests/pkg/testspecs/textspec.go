package testspecs

import (
	"os"
	"path/filepath"
	"strings"

	"k8s.io/klog/v2"
)

type TextSpecTranslator struct {
}

// New returns a Ginkgo Spec Translator
func NewTextSpecTranslator() *TextSpecTranslator {

	return &TextSpecTranslator{}
}

// FromFile generates a TestOutline from a Text outline File
func (tst *TextSpecTranslator) FromFile(file string) (TestOutline, error) {

	var outline TestOutline
	//var node TestSpecNode
	outlineData, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}
	outlineStr := string(outlineData)
	// Need this bit of code in case someone writes the text spec in a google doc and exports it as plain text file
	stringByteOrderMark := string('\uFEFF')
	if strings.ContainsRune(outlineStr, '\uFEFF') {
		outlineStr = strings.TrimPrefix(outlineStr, stringByteOrderMark)
	}

	for _, part := range strings.Split(outlineStr, "\n") {
		if !strings.Contains("\n", part) {
			node := createTestSpecNodesFromString(part)
			outline = graphNodeToTestSpecOutline(outline, node)
		}

	}

	return outline, nil
}

// ToFile generates a Text outline file from a TestOutline
func (tst *TextSpecTranslator) ToFile(destination string, outline TestOutline) error {
	dir := filepath.Dir(destination)
	err := os.MkdirAll(dir, 0775)
	if err != nil {
		klog.Errorf("failed to create package directory, %s, template with: %v", dir, err)
		return err
	}
	f, err := os.Create(destination)
	if err != nil {
		klog.Infof("Failed to create file: %v", err)
		return err
	}

	defer f.Close()

	err = os.WriteFile(destination, []byte(outline.ToString()), 0644)

	if err != nil {
		return err
	}
	klog.Infof("successfully written to %s", destination)

	return err

}

// createTestSpecNodesFromString takes a string line and builds
// a TestSpecNode that will be graphed later to form our tree
// outline
func createTestSpecNodesFromString(part string) TestSpecNode {

	outlineSlice := strings.Split(part, "\n")
	var node TestSpecNode
	for _, o := range outlineSlice {
		        // replace the carriage return if spec outline was initially generated from Windows/GDoc
		        o := strings.ReplaceAll(o, "\r", "")
		        whiteSpaces := len(o) - len(strings.TrimLeft(o, " "))
		        //skip over empty new lines
		        if whiteSpaces == 0 && len(o) == 0 {
		            continue
		        }
		
		        node = TestSpecNode{}
		        node.Name = strings.Trim(strings.Split(o, ":")[0], " ")
		        txt := strings.Trim(strings.Split(o, ":")[1], " ")
		        if strings.Contains(txt, "@") {
		            labels := strings.Split(txt, "@")[1:]
		            for _, l := range labels {
				noCommaL := strings.ReplaceAll(l, ",", "")
				node.Labels = append(node.Labels, strings.TrimRight(strings.TrimLeft(noCommaL, " "), " "))
				if strings.HasPrefix(l, "@") {
					node.Labels = append(node.Labels, strings.TrimLeft(l[1:], " "))
				}
			}
			txt = strings.Split(txt, "@")[0]
		}

		node.Text = txt
		node.Nodes = make(TestOutline, 0)
		node.LineSpaceLevel = whiteSpaces

	}

	return node

}

// graphNodeToTestSpecOutline will take the node built and graph it within the outline
// to create the hierarchical tree
func graphNodeToTestSpecOutline(nodes TestOutline, node TestSpecNode) TestOutline {

	if len(nodes) == 0 {

		nodes = append(nodes, node)
		return nodes
	}

	if len(nodes) > 0 {

		if nodes[len(nodes)-1].LineSpaceLevel == node.LineSpaceLevel {
			nodes = append(nodes, node)
			return nodes
		}

		if nodes[len(nodes)-1].LineSpaceLevel < node.LineSpaceLevel {
			nodes[len(nodes)-1].Nodes = graphNodeToTestSpecOutline(nodes[len(nodes)-1].Nodes, node)
			return nodes

		}
	}

	return nodes
}
