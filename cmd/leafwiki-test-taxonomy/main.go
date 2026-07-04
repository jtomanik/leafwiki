package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type reportOptions struct {
	Roots []string
}

type taxonomyReport struct {
	Roots        []string
	FilesScanned int
	SpecsScanned int
	LabeledSpecs int
	MissingSpecs []taxonomySpec
}

type taxonomySpec struct {
	Root        string
	File        string
	Line        int
	Description string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	roots := args
	if len(roots) == 0 {
		roots = []string{"."}
	}
	report, err := buildReport(reportOptions{Roots: roots})
	if err != nil {
		fmt.Fprintf(stderr, "leafwiki-test-taxonomy: %v\n", err)
		return 2
	}
	fmt.Fprint(stdout, formatReport(report))
	return 0
}

func buildReport(options reportOptions) (taxonomyReport, error) {
	report := taxonomyReport{Roots: append([]string(nil), options.Roots...)}
	absoluteRoots, err := absoluteRootPaths(options.Roots)
	if err != nil {
		return taxonomyReport{}, err
	}
	for _, root := range absoluteRoots {
		if err := scanRoot(root, absoluteRoots, &report); err != nil {
			return taxonomyReport{}, err
		}
	}
	sort.Slice(report.MissingSpecs, func(i int, j int) bool {
		left := report.MissingSpecs[i]
		right := report.MissingSpecs[j]
		if left.File != right.File {
			return left.File < right.File
		}
		return left.Line < right.Line
	})
	return report, nil
}

func absoluteRootPaths(roots []string) ([]string, error) {
	absoluteRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		absoluteRoots = append(absoluteRoots, filepath.Clean(absoluteRoot))
	}
	return absoluteRoots, nil
}

func scanRoot(absoluteRoot string, explicitRoots []string, report *taxonomyReport) error {
	return filepath.WalkDir(absoluteRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absoluteRoot && isExplicitRoot(path, explicitRoots) {
				return filepath.SkipDir
			}
			if shouldSkipDirectory(entry.Name()) && path != absoluteRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		specs, err := scanTestFile(absoluteRoot, path)
		if err != nil {
			return err
		}
		report.FilesScanned++
		for _, spec := range specs {
			report.SpecsScanned++
			if spec.HasPrimaryLabel {
				report.LabeledSpecs++
				continue
			}
			report.MissingSpecs = append(report.MissingSpecs, taxonomySpec{
				Root:        filepath.ToSlash(absoluteRoot),
				File:        spec.File,
				Line:        spec.Line,
				Description: spec.Description,
			})
		}
		return nil
	})
}

func isExplicitRoot(path string, roots []string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		if path == root {
			return true
		}
	}
	return false
}

func shouldSkipDirectory(name string) bool {
	switch name {
	case "references", "node_modules", "testdata":
		return true
	default:
		return false
	}
}

type scannedSpec struct {
	File            string
	Line            int
	Description     string
	HasPrimaryLabel bool
}

func scanTestFile(root string, path string) ([]scannedSpec, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	parents := buildParentMap(file)
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	relativePath = filepath.ToSlash(relativePath)

	var specs []scannedSpec
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		name := callName(call)
		if !isRunnableGinkgoCall(name) {
			return true
		}
		labels := primaryLabelsForCall(parents, call)
		specs = append(specs, scannedSpec{
			File:            relativePath,
			Line:            fset.Position(call.Pos()).Line,
			Description:     ginkgoDescription(call),
			HasPrimaryLabel: len(labels) > 0,
		})
		return true
	})
	return specs, nil
}

func buildParentMap(file *ast.File) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if len(stack) > 0 {
			parents[node] = stack[len(stack)-1]
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func primaryLabelsForCall(parents map[ast.Node]ast.Node, call *ast.CallExpr) []string {
	labels := taxonomyLabelSet{}
	for _, ancestor := range enclosingGinkgoCalls(parents, call) {
		labels.addAll(directPrimaryLabels(ancestor))
	}
	labels.addAll(directPrimaryLabels(call))
	return labels.labels
}

func enclosingGinkgoCalls(parents map[ast.Node]ast.Node, call *ast.CallExpr) []*ast.CallExpr {
	var calls []*ast.CallExpr
	for current := parents[call]; current != nil; current = parents[current] {
		enclosingCall, ok := current.(*ast.CallExpr)
		if !ok {
			continue
		}
		name := callName(enclosingCall)
		if isGinkgoContainerCall(name) || isDescribeTableCall(name) {
			calls = append(calls, enclosingCall)
		}
	}
	for left, right := 0, len(calls)-1; left < right; left, right = left+1, right-1 {
		calls[left], calls[right] = calls[right], calls[left]
	}
	return calls
}

func directPrimaryLabels(call *ast.CallExpr) []string {
	labels := taxonomyLabelSet{}
	for _, decorator := range labelDecorators(call) {
		for _, arg := range decorator.Args {
			label, ok := stringLiteralValue(arg)
			if ok && isPrimaryTaxonomyLabel(label) {
				labels.add(label)
			}
		}
	}
	return labels.labels
}

func labelDecorators(call *ast.CallExpr) []*ast.CallExpr {
	var decorators []*ast.CallExpr
	for _, arg := range call.Args {
		decorator, ok := unparenExpr(arg).(*ast.CallExpr)
		if ok && callName(decorator) == "Label" {
			decorators = append(decorators, decorator)
		}
	}
	return decorators
}

func ginkgoDescription(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return "<missing description>"
	}
	description, ok := stringLiteralValue(call.Args[0])
	if ok {
		return description
	}
	return "<dynamic description>"
}

func isRunnableGinkgoCall(name string) bool {
	switch name {
	case "It", "Specify", "FIt", "FSpecify", "Entry", "FEntry":
		return true
	default:
		return false
	}
}

func isGinkgoContainerCall(name string) bool {
	switch name {
	case "Describe", "Context", "When",
		"FDescribe", "FContext", "FWhen":
		return true
	default:
		return false
	}
}

func isDescribeTableCall(name string) bool {
	switch name {
	case "DescribeTable", "FDescribeTable":
		return true
	default:
		return false
	}
}

func isPrimaryTaxonomyLabel(label string) bool {
	switch label {
	case "unit", "integration", "e2e":
		return true
	default:
		return false
	}
}

func callName(call *ast.CallExpr) string {
	switch fun := unparenExpr(call.Fun).(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	default:
		return ""
	}
}

func unparenExpr(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func stringLiteralValue(expr ast.Expr) (string, bool) {
	lit, ok := unparenExpr(expr).(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

type taxonomyLabelSet struct {
	labels []string
	seen   map[string]bool
}

func (set *taxonomyLabelSet) add(label string) {
	if set.seen == nil {
		set.seen = map[string]bool{}
	}
	if set.seen[label] {
		return
	}
	set.seen[label] = true
	set.labels = append(set.labels, label)
}

func (set *taxonomyLabelSet) addAll(labels []string) {
	for _, label := range labels {
		set.add(label)
	}
}

func formatReport(report taxonomyReport) string {
	var out bytes.Buffer
	fmt.Fprintln(&out, "LeafWiki Go/Ginkgo taxonomy report")
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "roots:")
	for _, root := range report.Roots {
		fmt.Fprintf(&out, "- %s\n", root)
	}
	fmt.Fprintln(&out)
	fmt.Fprintf(&out, "scanned test files: %d\n", report.FilesScanned)
	fmt.Fprintf(&out, "runnable Ginkgo specs: %d\n", report.SpecsScanned)
	fmt.Fprintf(&out, "labeled taxonomy specs: %d\n", report.LabeledSpecs)
	fmt.Fprintf(&out, "missing taxonomy labels: %d\n", len(report.MissingSpecs))
	if len(report.MissingSpecs) == 0 {
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, "No missing taxonomy labels found.")
		return out.String()
	}
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "missing specs:")
	currentFile := ""
	for _, spec := range report.MissingSpecs {
		if spec.File != currentFile {
			currentFile = spec.File
			fmt.Fprintf(&out, "%s\n", currentFile)
		}
		fmt.Fprintf(&out, "  line %d: %s\n", spec.Line, spec.Description)
	}
	return out.String()
}
