package testspecs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"

	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/inspector"
	"k8s.io/klog"
)

// ExtractFrameworkDescribeNode will return a TestSpecNode
func ExtractFrameworkDescribeNode(filename string) (TestSpecNode, error) {
	ispr, err := parseGinkgoAst(filename)
	if err != nil {
		return TestSpecNode{}, err
	}
	return getFrameworkDescribeNode(ispr), nil
}

// parseGinkgoAst creates a new AST inspector based on
// the ginkgo go file it was passed
func parseGinkgoAst(filename string) (inspector.Inspector, error) {

	fset := token.NewFileSet()
	src, err := os.Open(filename)
	if err != nil {
		klog.Errorf("Failed to open file %s", filename)
		return inspector.Inspector{}, err
	}
	defer src.Close()
	parsedSrc, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		klog.Errorf("Failed to parse file to inspect %s", filename)
		return inspector.Inspector{}, err
	}
	ispr := inspector.New([]*ast.File{parsedSrc})

	return *ispr, nil

}

// getFrameworkDescribeNode will use the AST inspector to search
// for the framework describe decorator function within the test file
// so that we can generate a complete outline
func getFrameworkDescribeNode(isp inspector.Inspector) TestSpecNode {

	var gnode TestSpecNode
	isp.Preorder([]ast.Node{&ast.CallExpr{}}, func(n ast.Node) {
		g := findFrameworkDescribeAstNode(n.(*ast.CallExpr))
		if g.Name != "" {
			gnode = g
		}
	})

	return gnode
}

// findFrameworkDescribeAstNode will examine the call expression
// to determine if it is the framework describe decorator
// and generate a TestSpecNode for it
func findFrameworkDescribeAstNode(ce *ast.CallExpr) TestSpecNode {

	var funcname string
	var n = TestSpecNode{}
	switch expr := ce.Fun.(type) {
	case *ast.Ident:
		funcname = expr.Name
	case *ast.SelectorExpr:
		funcname = expr.Sel.Name
	}

	if strings.Contains(funcname, "Describe") && len(funcname) > 8 && !strings.Contains(funcname, "DescribeTable") {
		n.Name = funcname
		text, ok := ce.Args[0].(*ast.BasicLit)
		if !ok {
			// some of our tests don't provide a string to this function
			// so follow ginkgo outline and set it to `undefined`
			n.Text = "undefined"
			n.Labels = extractFrameworkDescribeLabels(ce)
			return n
		}
		switch text.Kind {
		case token.CHAR, token.STRING:
			// For token.CHAR and token.STRING, Value is quoted
			unquoted, err := strconv.Unquote(text.Value)
			if err != nil {
				// If unquoting fails, just use the raw Value
				n.Text = text.Value
			}
			n.Text = unquoted
		default:
			n.Text = text.Value
		}
		n.Labels = extractFrameworkDescribeLabels(ce)

	}

	return n

}

// extractFrameworkDescribeLables iterates through the Call Expression
// to determine if it is a Ginkgo Label
func extractFrameworkDescribeLabels(ce *ast.CallExpr) []string {

	labels := []string{}

	for _, arg := range ce.Args {
		switch expr := arg.(type) {
		case *ast.CallExpr:
			id, ok := expr.Fun.(*ast.Ident)
			if !ok {
				// to skip over cases where the expr.Fun. is actually *ast.SelectorExpr
				continue
			}
			if id.Name == "Label" {
				ls := extractLabels(expr)
				labels = append(labels, ls...)
			}
		}
	}
	return labels

}

// extracLabels will extract the string values from the
// Ginkgo Label expression
func extractLabels(ce *ast.CallExpr) []string {

	out := []string{}
	for _, arg := range ce.Args {
		switch expr := arg.(type) {
		case *ast.BasicLit:
			if expr.Kind == token.STRING {
				unquoted, err := strconv.Unquote(expr.Value)
				if err != nil {
					unquoted = expr.Value
				}
				out = append(out, unquoted)
			}
		}
	}

	return out

}
