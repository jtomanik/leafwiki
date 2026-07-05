package testhygiene

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/analysis"
)

type ruleHarness struct {
	ctx         *analysisContext
	pass        *analysis.Pass
	file        *ast.File
	diagnostics []analysis.Diagnostic
}

func newRuleHarness(filename string, packagePath string, src string) *ruleHarness {
	ginkgo.GinkgoHelper()

	return newRuleHarnessWithFiles(filename, packagePath, map[string]string{filename: src})
}

func newRuleHarnessWithFiles(targetFilename string, packagePath string, sources map[string]string) *ruleHarness {
	ginkgo.GinkgoHelper()

	fset := token.NewFileSet()
	filenames := make([]string, 0, len(sources))
	for filename := range sources {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)

	files := make([]*ast.File, 0, len(filenames))
	var targetFile *ast.File
	for _, filename := range filenames {
		file, err := parser.ParseFile(fset, filename, sources[filename], parser.ParseComments)
		Expect(err).NotTo(HaveOccurred())
		files = append(files, file)
		if filename == targetFilename {
			targetFile = file
		}
	}
	Expect(targetFile).NotTo(BeNil(), "target file %q should exist in harness sources", targetFilename)

	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var typeErrors []string
	config := &types.Config{
		Importer: importer.Default(),
		Error: func(err error) {
			typeErrors = append(typeErrors, err.Error())
		},
	}
	pkg, err := config.Check(packagePath, fset, files, info)
	Expect(err).NotTo(HaveOccurred(), strings.Join(typeErrors, "\n"))

	harness := &ruleHarness{file: targetFile}
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     files,
		Pkg:       pkg,
		TypesInfo: info,
		Report: func(diagnostic analysis.Diagnostic) {
			harness.diagnostics = append(harness.diagnostics, diagnostic)
		},
	}
	harness.pass = pass
	harness.ctx = newAnalysisContext(pass)
	return harness
}

func (h *ruleHarness) diagnosticMessages() []string {
	ginkgo.GinkgoHelper()

	h.ctx.finalizeDiagnostics()
	messages := make([]string, 0, len(h.diagnostics))
	for _, diagnostic := range h.diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	return messages
}

func (h *ruleHarness) findCall(name string) *ast.CallExpr {
	ginkgo.GinkgoHelper()

	calls := h.findCalls(name)
	return calls[0]
}

func (h *ruleHarness) findCalls(name string) []*ast.CallExpr {
	ginkgo.GinkgoHelper()

	var found []*ast.CallExpr
	ast.Inspect(h.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && callName(call) == name {
			found = append(found, call)
		}
		return true
	})
	Expect(found).To(ContainElement(Not(BeNil())), "call %q should exist", name)
	return found
}

func (h *ruleHarness) findFunc(name string) *ast.FuncDecl {
	ginkgo.GinkgoHelper()

	var found *ast.FuncDecl
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		fn, ok := node.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			found = fn
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "function %q should exist", name)
	return found
}

func (h *ruleHarness) findGoStmt() *ast.GoStmt {
	ginkgo.GinkgoHelper()

	var found *ast.GoStmt
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		stmt, ok := node.(*ast.GoStmt)
		if ok {
			found = stmt
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "go statement should exist")
	return found
}

func (h *ruleHarness) findBlockingReceive() *ast.UnaryExpr {
	ginkgo.GinkgoHelper()

	var found *ast.UnaryExpr
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		expr, ok := node.(*ast.UnaryExpr)
		if ok && expr.Op == token.ARROW {
			found = expr
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "blocking receive should exist")
	return found
}
