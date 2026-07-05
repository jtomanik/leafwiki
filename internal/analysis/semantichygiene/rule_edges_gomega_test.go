package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"sort"
	"strconv"
	"strings"
)

type ruleHarness struct {
	ctx         *analysisContext
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
	harness.ctx = newAnalysisContext(pass)
	return harness
}

func (h *ruleHarness) resetDiagnostics() {
	h.diagnostics = nil
	h.ctx.diagnostics = nil
}

func (h *ruleHarness) diagnosticMessages() []string {
	ginkgo.GinkgoHelper()

	messages := make([]string, 0, len(h.diagnostics)+len(h.ctx.diagnostics))
	for _, diagnostic := range h.diagnostics {
		messages = append(messages, diagnostic.Message)
	}
	for _, diagnostic := range h.ctx.diagnostics {
		messages = append(messages, formatDiagnosticMessage(diagnostic))
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

func (h *ruleHarness) findTypeSpec(name string) *ast.TypeSpec {
	ginkgo.GinkgoHelper()

	var found *ast.TypeSpec
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		spec, ok := node.(*ast.TypeSpec)
		if ok && spec.Name.Name == name {
			found = spec
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "type %q should exist", name)
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

func (h *ruleHarness) findKeyValue(key string) *ast.KeyValueExpr {
	ginkgo.GinkgoHelper()

	var found *ast.KeyValueExpr
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		kv, ok := node.(*ast.KeyValueExpr)
		if ok && keyName(kv.Key) == key {
			found = kv
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "key %q should exist", key)
	return found
}

func (h *ruleHarness) findLiteral(value string) *ast.BasicLit {
	ginkgo.GinkgoHelper()

	var found *ast.BasicLit
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		got, err := strconv.Unquote(lit.Value)
		if err == nil && got == value {
			found = lit
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "literal %q should exist", value)
	return found
}

func (h *ruleHarness) firstCompositeLiteral() *ast.CompositeLit {
	ginkgo.GinkgoHelper()

	var found *ast.CompositeLit
	ast.Inspect(h.file, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		lit, ok := node.(*ast.CompositeLit)
		if ok {
			found = lit
			return false
		}
		return true
	})
	Expect(found).NotTo(BeNil(), "composite literal should exist")
	return found
}

func namedStringType(name string) *types.Named {
	pkg := types.NewPackage("example.com/semantics", "semantics")
	return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), types.Typ[types.String], nil)
}

type ruleBranchDecision int

const (
	ruleBranchRejected ruleBranchDecision = iota
	ruleBranchAccepted
)

type namedClassification struct {
	Name     string
	Decision ruleBranchDecision
}

func decisionFor(accepted bool) ruleBranchDecision {
	if accepted {
		return ruleBranchAccepted
	}
	return ruleBranchRejected
}

func semanticTypeClassification(t types.Type) namedClassification {
	name, ok := semanticTypeNameOf(t)
	return namedClassification{Name: name, Decision: decisionFor(ok)}
}

func primitiveCarrierClassification(t types.Type) namedClassification {
	name, ok := primitiveCarrierTypeName(t)
	return namedClassification{Name: name, Decision: decisionFor(ok)}
}

func unnamedParamClassification(funcName string, typeName string, index int) namedClassification {
	paramName, semanticTypeName, ok := semanticTypeForUnnamedParam(funcName, typeName, index)
	return namedClassification{Name: paramName + semanticTypeName, Decision: decisionFor(ok)}
}
