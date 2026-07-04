package semantichygiene

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	Expect(len(found)).To(BeNumerically(">", 0), "call %q should exist", name)
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

var _ = ginkgo.Describe("semantichygiene diagnostic edge cases", func() {
	ginkgo.Describe("structured diagnostics", func() {
		ginkgo.It("delays rule diagnostics until finalization and prefixes the stable rule ID", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func TestPage() {}
`)

			h.ctx.report(ruleDirectCast, h.file.Name, directCastDiagnostic("WorkspaceID"))
			Expect(h.diagnostics).To(BeEmpty())

			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:semantic.direct-cast: direct cast to semantic type WorkspaceID outside parser or boundary; use a parser or typed input",
			))
		})

		ginkgo.It("formats every registered rule with its stable rule ID prefix", func() {
			for id := range allRuleMetadata() {
				message := formatDiagnosticMessage(semanticDiagnostic{
					rule:    id,
					message: "diagnostic text",
				})

				Expect(message).To(HavePrefix("semh:" + string(id) + ": "))
			}
		})

		ginkgo.It("suppresses one matching call-scoped waivable diagnostic", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer here
var _ = It("documents package invariant", func() {})
`)

			h.ctx.report(ruleGinkgoTopLevelIt, h.findCall("It"), "top-level It reads like a migrated unit test; place it under a behavior container")
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnostics).To(BeEmpty())
		})

		ginkgo.It("suppresses a matcher diagnostic when the waiver is before the outer assertion call", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func Expect(actual any) assertion { return assertion{} }
type assertion struct{}
func (assertion) To(matcher any) {}
func Equal(expected any) any { return nil }

func TestPage() {
	// semh:allow gomega.equal-zero -- zero literal reads clearer in this compatibility assertion
	Expect(0).To(
		Equal(0),
	)
}
`)
			equal := h.findCall("Equal")

			h.ctx.report(ruleGomegaEqualZero, equal, gomegaEqualZeroDiagnostic())
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnostics).To(BeEmpty())
		})

		ginkgo.It("does not let a call-scoped waiver before a spec suppress diagnostics inside the spec body", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }
func Expect(actual any) assertion { return assertion{} }
type assertion struct{}
func (assertion) To(matcher any) {}
func Equal(expected any) any { return nil }

// semh:allow gomega.equal-zero -- the spec node must not waive assertions in its body
var _ = It("documents behavior", func() {
	Expect(0).To(Equal(0))
})
`)

			h.ctx.report(ruleGomegaEqualZero, h.findCall("Equal"), gomegaEqualZeroDiagnostic())
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.stale: semh waiver for gomega.equal-zero did not match any diagnostic",
				"semh:gomega.equal-zero: use BeZero matcher instead of Equal(0) for zero-value assertions",
			))
		})

		ginkgo.It("suppresses one matching declaration-scoped waivable diagnostic", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow gomega.helper-should-be-matcher -- compact helper reads clearer than a matcher here
func assertResponse() {}
`)

			h.ctx.report(ruleGomegaHelperShouldBeMatcher, h.findFunc("assertResponse").Name, "prefer a custom Gomega matcher for reusable assertion helper assertResponse")
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnostics).To(BeEmpty())
		})

		ginkgo.It("matches next-node waivers only against the immediately following node", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- exercises next-node scope
func TestPage() {}
func TestLater() {}
`)
			waivers, diagnostics := h.ctx.collectWaivers()
			Expect(diagnostics).To(BeEmpty())
			Expect(waivers).To(HaveLen(1))

			immediate := h.findFunc("TestPage")
			later := h.findFunc("TestLater")
			Expect(h.ctx.waiverMatchesDiagnostic(waivers[0], semanticDiagnostic{
				rule: ruleGinkgoTopLevelIt,
				pos:  immediate.Pos(),
				node: immediate,
			}, waiverScopeNextNode)).To(BeTrue())
			Expect(h.ctx.waiverMatchesDiagnostic(waivers[0], semanticDiagnostic{
				rule: ruleGinkgoTopLevelIt,
				pos:  later.Pos(),
				node: later,
			}, waiverScopeNextNode)).To(BeFalse())
		})

		ginkgo.It("reports a valid waiver that matches no diagnostic as stale", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer here
func TestPage() {}
`)

			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.stale: semh waiver for ginkgo.top-level-it did not match any diagnostic",
			))
		})

		ginkgo.It("reports adjacent same-rule waivers for one diagnostic as duplicate", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first explanation
// semh:allow ginkgo.top-level-it -- duplicate explanation
var _ = It("documents package invariant", func() {})
`)

			h.ctx.report(ruleGinkgoTopLevelIt, h.findCall("It"), "top-level It reads like a migrated unit test; place it under a behavior container")
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.duplicate: duplicate semh waiver for ginkgo.top-level-it; one waiver can suppress one diagnostic",
			))
		})

		ginkgo.It("reports non-waivable rule waivers and leaves the hard diagnostic active", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow semantic.direct-cast -- this must stay hard
func TestPage() {}
`)

			h.ctx.report(ruleDirectCast, h.file.Name, directCastDiagnostic("WorkspaceID"))
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.non-waivable-rule: semh waiver for semantic.direct-cast cannot suppress hard diagnostics",
				"semh:semantic.direct-cast: direct cast to semantic type WorkspaceID outside parser or boundary; use a parser or typed input",
			))
		})

		ginkgo.It("reports active waivers that exceed a per-rule budget", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func It(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first package-level invariant
var _ = It("documents first package invariant", func() {})
// semh:allow ginkgo.top-level-it -- second package-level invariant
var _ = It("documents second package invariant", func() {})
// semh:allow ginkgo.top-level-it -- third package-level invariant
var _ = It("documents third package invariant", func() {})
// semh:allow ginkgo.top-level-it -- fourth package-level invariant
var _ = It("documents fourth package invariant", func() {})
`)
			for _, call := range h.findCalls("It") {
				h.ctx.report(ruleGinkgoTopLevelIt, call, "top-level It reads like a migrated unit test; place it under a behavior container")
			}

			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.budget-exceeded: waiver budget exceeded for ginkgo.top-level-it: used 4, budget 3",
			))
		})

		ginkgo.It("reports active waivers that exceed the total budget", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

func Node(text string, body func()) bool { return true }

// semh:allow ginkgo.top-level-it -- first package-level invariant
var _ = Node("node 1", func() {})
// semh:allow ginkgo.top-level-it -- second package-level invariant
var _ = Node("node 2", func() {})
// semh:allow ginkgo.top-level-it -- third package-level invariant
var _ = Node("node 3", func() {})
// semh:allow ginkgo.wide-entry -- first wide entry exception
var _ = Node("node 4", func() {})
// semh:allow ginkgo.wide-entry -- second wide entry exception
var _ = Node("node 5", func() {})
// semh:allow ginkgo.wide-entry -- third wide entry exception
var _ = Node("node 6", func() {})
// semh:allow gomega.equal-empty -- first empty matcher exception
var _ = Node("node 7", func() {})
// semh:allow gomega.equal-empty -- second empty matcher exception
var _ = Node("node 8", func() {})
// semh:allow gomega.equal-empty -- third empty matcher exception
var _ = Node("node 9", func() {})
// semh:allow gomega.equal-zero -- first zero matcher exception
var _ = Node("node 10", func() {})
// semh:allow gomega.equal-zero -- second zero matcher exception
var _ = Node("node 11", func() {})
`)
			rules := []ruleID{
				ruleGinkgoTopLevelIt,
				ruleGinkgoTopLevelIt,
				ruleGinkgoTopLevelIt,
				ruleGinkgoWideEntry,
				ruleGinkgoWideEntry,
				ruleGinkgoWideEntry,
				ruleGomegaEqualEmpty,
				ruleGomegaEqualEmpty,
				ruleGomegaEqualEmpty,
				ruleGomegaEqualZero,
				ruleGomegaEqualZero,
			}
			for i, call := range h.findCalls("Node") {
				h.ctx.report(rules[i], call, "waivable readability diagnostic")
			}

			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:waiver.budget-exceeded: total waiver budget exceeded: used 11, budget 10",
			))
		})

		ginkgo.It("reports package-qualified top-level It calls without a file-wide container prerequisite", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.It("documents package invariant", func() {})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
			))
		})

		ginkgo.It("reports dot-imported top-level It calls", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

func It(text string, body func()) bool { return true }

var _ = It("documents package invariant", func() {})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.Ident)] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
			))
		})

		ginkgo.It("reports migrated GinkgoT wrappers", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }
func (fakeT) Helper() {}

var _ = ginkgo.It("TestExistingMigratedSpec", func() {
	t := ginkgo.GinkgoT()
	t.Helper()
})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
				`semh:ginkgo.test-name: Ginkgo node name "TestExistingMigratedSpec" preserves a migrated testing.T name; describe observable behavior instead`,
				"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Helper adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("reports top-level It in ordinary repo packages", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.It("documents existing migrated behavior", func() {})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason",
			))
		})

		ginkgo.It("ignores local selector calls on an identifier named ginkgo", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("page behavior", func() {})
var _ = ginkgo.It("documents local DSL behavior", func() {})
`)

			checkGinkgoSpecQualityCall(h.ctx, h.findCall("It"))

			Expect(h.diagnosticMessages()).To(BeEmpty())
		})

		ginkgo.It("reports Ginkgo spec names that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("TestFormatsBrand", func() {})
})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestFormatsBrand" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports Ginkgo spec names that preserve Go code symbols", func() {
			h := newRuleHarness("/repo/internal/wikid/wikid_helpers_test.go", "github.com/perber/wiki/internal/wikid", `package wikid

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("wikid process supervision", func() {
	ginkgo.It("Supervisor.Roles returns a snapshot of marked roles", func() {})
})
`)
			call := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "Supervisor.Roles returns a snapshot of marked roles" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports helper behavior names as vague migration residue", func() {
			h := newRuleHarness("/repo/internal/wikid/wikid_helpers_test.go", "github.com/perber/wiki/internal/wikid", `package wikid

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }

var _ = ginkgo.Describe("wikid helper behavior", func() {})
`)
			call := h.findCall("Describe")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Describe",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.vague-name: Ginkgo node name "wikid helper behavior" is too vague to document behavior; describe the observable outcome instead`,
			))
		})

		ginkgo.It("reports Ginkgo container names that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }

var _ = ginkgo.Describe("TestBrandRendering", func() {})
`)
			call := h.findCall("Describe")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Describe",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestBrandRendering" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports Ginkgo table names that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }

var _ = ginkgo.DescribeTable("TestBrandRows", func() {})
`)
			call := h.findCall("DescribeTable")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"DescribeTable",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestBrandRows" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports Ginkgo entry names that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.Entry("TestAcceptedBrand", 1)
`)
			call := h.findCall("Entry")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Entry",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports Ginkgo entry description names that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Entry(text any, args ...any) bool { return true }
func (bddDSL) EntryDescription(text string) string { return text }

var _ = ginkgo.Entry(ginkgo.EntryDescription("TestAcceptedBrand"), 1)
`)
			call := h.findCall("Entry")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Entry",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports Ginkgo table entry description decorators that preserve migrated Test function names", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text any, args ...any) bool { return true }
func (bddDSL) EntryDescription(text string) string { return text }

var _ = ginkgo.DescribeTable("brand rows", func() {},
	ginkgo.EntryDescription("TestAcceptedBrand"),
	ginkgo.Entry(nil, 1),
)
`)
			call := h.findCall("DescribeTable")
			h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"DescribeTable",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, call)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
			))
		})

		ginkgo.It("reports GinkgoT adapters inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		_ = ginkgo.GinkgoT()
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			adapter := h.findCall("GinkgoT")
			h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"GinkgoT",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid GinkgoT adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("reports GinkgoT adapters inside Ginkgo table bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

var _ = ginkgo.DescribeTable("brand rows", func() {
	_ = ginkgo.GinkgoT()
}, ginkgo.Entry("accepted"))
`)
			table := h.findCall("DescribeTable")
			h.ctx.pass.TypesInfo.Uses[table.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"DescribeTable",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			adapter := h.findCall("GinkgoT")
			h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"GinkgoT",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, table)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid GinkgoT adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("reports testing.T Fatalf assertions inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		t.Fatalf("brand did not format")
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("reports testing.T Fatalf assertions inside Ginkgo table bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.DescribeTable("brand rows", func() {
	t.Fatalf("brand did not format")
}, ginkgo.Entry("accepted"))
`)
			table := h.findCall("DescribeTable")
			h.ctx.pass.TypesInfo.Uses[table.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"DescribeTable",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, table)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("reports local testing.T-style adapters inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/tree/tree_test.go", "github.com/perber/wiki/internal/tree", `package tree

type bddDSL struct{}
type treeTestT interface {
	Fatalf(format string, args ...any)
	TempDir() string
}
type ginkgoTreeT struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (ginkgoTreeT) Fatalf(format string, args ...any) {}
func (ginkgoTreeT) TempDir() string { return "" }
func treeSpecT() treeTestT { return ginkgoTreeT{} }

var _ = ginkgo.Describe("tree behavior", func() {
	ginkgo.It("reconstructs pages", func() {
		t := treeSpecT()
		t.Fatalf("tree did not reconstruct")
		_ = t.TempDir()
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
				"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.TempDir adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("does not treat logger Error and Log methods as testing.T-style adapters", func() {
			h := newRuleHarness("/repo/internal/logging/logging_test.go", "github.com/perber/wiki/internal/logging", `package logging

type bddDSL struct{}
type logger interface {
	Error(message string)
	Log(message string)
}
type testLogger struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (testLogger) Error(message string) {}
func (testLogger) Log(message string) {}
func newLogger() logger { return testLogger{} }

var _ = ginkgo.Describe("logging behavior", func() {
	ginkgo.It("writes an error-level log entry", func() {
		logger := newLogger()
		logger.Error("failed")
		logger.Log("debug")
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(BeEmpty())
		})

		ginkgo.It("reports direct fail calls inside spec bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) Fail(message string) {}

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		ginkgo.Fail("brand did not format")
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			fail := h.findCall("Fail")
			h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Fail",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.fail-in-spec: avoid direct ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
			))
		})

		ginkgo.It("reports direct fail calls inside callbacks nested in spec bodies", func() {
			h := newRuleHarness("/repo/internal/frontd/frontd_test.go", "github.com/perber/wiki/internal/frontd", `package frontd

import "net/http"

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) Fail(message string) {}

func NewHandler(handler http.HandlerFunc) http.Handler { return handler }

var _ = ginkgo.Describe("private handler", func() {
	ginkgo.It("rejects private requests before reaching the upstream handler", func() {
		handler := NewHandler(func(w http.ResponseWriter, req *http.Request) {
			ginkgo.Fail("upstream handler was called")
		})
		_ = handler
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			fail := h.findCall("Fail")
			h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Fail",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.fail-in-spec: avoid direct ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
			))
		})

		ginkgo.It("reports local failure helpers inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/tree/tree_test.go", "github.com/perber/wiki/internal/tree", `package tree

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func failTreeSpec(format string, args ...any) {}

var _ = ginkgo.Describe("tree behavior", func() {
	ginkgo.It("reconstructs pages", func() {
		if true {
			failTreeSpec("tree did not reconstruct")
		}
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.fail-in-spec: avoid failure helper failTreeSpec inside specs; use Gomega expectations so assertions read semantically",
			))
		})

		ginkgo.It("reports helpers that hide ginkgo.Fail inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/mcp/mcp_test.go", "github.com/perber/wiki/internal/mcp", `package mcp

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoHelper() {}
func (bddDSL) Fail(message string) {}

func requiredPrivateActorContextFailure() {
	ginkgo.GinkgoHelper()
	ginkgo.Fail("private actor context did not handle the request")
}

var _ = ginkgo.Describe("private actor context", func() {
	ginkgo.It("accepts trusted private actor headers", func() {
		requiredPrivateActorContextFailure()
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			fail := h.findCall("Fail")
			h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"Fail",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.fail-in-spec: avoid helper requiredPrivateActorContextFailure that calls ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
			))
		})

		ginkgo.It("reports goroutine assertions without recovery inside Ginkgo table bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type assertion struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func Expect(actual any) assertion { return assertion{} }
func BeTrue() any { return nil }
func (assertion) To(matcher any) {}

var _ = ginkgo.DescribeTable("brand rows", func() {
	go func() {
		Expect(true).To(BeTrue())
	}()
}, ginkgo.Entry("accepted"))
`)
			checkGinkgoGoroutineAssertionRecovery(h.ctx, h.findGoStmt())

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.goroutine-recover: goroutine with assertions must defer GinkgoRecover() or use GinkgoHelperGo",
			))
		})

		ginkgo.It("reports blocking receives inside Ginkgo table bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.DescribeTable("brand rows", func() {
	ch := make(chan string)
	<-ch
}, ginkgo.Entry("accepted"))
`)
			checkGinkgoBlockingReceive(h.ctx, h.findBlockingReceive())

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.blocking-receive: avoid blocking channel receives in specs; use Eventually(...).Should(Receive(...)) so failures surface",
			))
		})

		ginkgo.It("reports async assertions without context inside Ginkgo table bodies with SpecContext", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type SpecContext struct{}
type asyncAssertion struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(SpecContext), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func Eventually(actual any, args ...any) asyncAssertion { return asyncAssertion{} }
func Equal(want any) any { return nil }
func (asyncAssertion) Should(matcher any) {}

var _ = ginkgo.DescribeTable("brand rows", func(ctx SpecContext) {
	Eventually(func() int { return 1 }).Should(Equal(1))
}, ginkgo.Entry("accepted"))
`)
			checkGomegaAsyncAssertion(h.ctx, h.findCall("Should"))

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.async-context: propagate the spec context into Eventually/Consistently with WithContext or positional context",
			))
		})

		ginkgo.It("reports testing.T fatal and error assertions inside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		t.Fatal("brand did not format")
		t.Errorf("brand did not format")
		t.Error("brand did not format")
	})
})
`)
			spec := h.findCall("It")
			h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"It",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, spec)

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatal assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
				"semh:ginkgo.testing-t-in-spec: avoid testing.T.Errorf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
				"semh:ginkgo.testing-t-in-spec: avoid testing.T.Error assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			))
		})

		ginkgo.It("does not report testing.T usage outside Ginkgo spec bodies", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) BeforeEach(body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

func helper(t *testing.T) {
	t.Fatalf("helper failure")
}

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.BeforeEach(func() {
		_ = ginkgo.GinkgoT()
		t.Fatalf("setup failure")
	})
})
`)
			setup := h.findCall("BeforeEach")
			h.ctx.pass.TypesInfo.Uses[setup.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"BeforeEach",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)
			adapter := h.findCall("GinkgoT")
			h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
				token.NoPos,
				types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
				"GinkgoT",
				types.NewSignatureType(nil, nil, nil, nil, nil, false),
			)

			checkGinkgoSpecQualityCall(h.ctx, setup)
			checkGinkgoSpecQualityCall(h.ctx, adapter)
			for _, call := range h.findCalls("Fatalf") {
				checkGinkgoSpecQualityCall(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(BeEmpty())
		})

		ginkgo.It("reports raw ginkgolinter ignore comments as hard semantic-hygiene violations", func() {
			h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

// ginkgo-linter:ignore-len-assertion
func helper() {}
`)
			h.ctx.finalizeDiagnostics()

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:ginkgo-linter.raw-ignore: raw ginkgolinter ignore comments are not allowed; fix the generic lint or use semh waivers only for waivable semantic-hygiene rules",
			))
		})

		ginkgo.It("reports boolean literal assertions that force pass or fail", func() {
			h := newRuleHarness("/repo/cmd/leafwiki/main_test.go", "github.com/perber/wiki/cmd/leafwiki", `package main

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }

func TestCLIBehavior() {
	Expect(false).To(BeTrue())
	Expect(true).To(BeFalse())
	privateURLMissing := true
	privateTokenMissing := false
	Expect(privateURLMissing || privateTokenMissing).To(BeFalse())
}
`)
			for _, call := range h.findCalls("To") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.boolean-literal: use semantic Gomega assertions instead of forcing pass/fail with boolean literals",
				"semh:gomega.boolean-literal: use semantic Gomega assertions instead of forcing pass/fail with boolean literals",
				"semh:gomega.binary-boolean: use semantic Gomega matchers instead of asserting binary expressions with BeTrue/BeFalse",
			))
		})

		ginkgo.It("reports boolean literal Equal matchers inside structured matcher values", func() {
			h := newRuleHarness("/repo/internal/wiki/pages/routes_handlers_gomega_test.go", "github.com/perber/wiki/internal/wiki/pages", `package pages

type assertion struct{}
type Fields map[string]any
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func MatchFields(options any, fields Fields) any { return nil }

type pathLookup struct {
	Exists bool
	Visible bool
}

func matchExistingRoutePathLookup() any {
	return MatchFields(nil, Fields{
		"Exists": Equal(true),
		"Visible": Equal(false),
	})
}

func TestRouteLookup() {
	Expect(pathLookup{Exists: true}).To(matchExistingRoutePathLookup())
}
`)
			for _, call := range h.findCalls("Equal") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.boolean-literal: use BeTrue/BeFalse instead of Equal(true/false) for boolean values",
				"semh:gomega.boolean-literal: use BeTrue/BeFalse instead of Equal(true/false) for boolean values",
			))
		})

		ginkgo.It("reports os.IsNotExist hidden behind WithTransform boolean matchers", func() {
			h := newRuleHarness("/repo/internal/wiki/wiki_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "os"

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func WithTransform(transform any, matcher any) any { return nil }
func BeTrue() any { return nil }

func TestWikiBehavior() {
	var err error
	Expect(err).To(WithTransform(os.IsNotExist, BeTrue()))
	Expect("missing.md").To(WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, WithTransform(os.IsNotExist, BeTrue())))
}
`)
			for _, call := range h.findCalls("WithTransform") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.os-is-not-exist-matcher: assert error semantics with MatchError instead of os.IsNotExist(...) with BeTrue/BeFalse",
				"semh:gomega.os-is-not-exist-matcher: assert error semantics with MatchError instead of os.IsNotExist(...) with BeTrue/BeFalse",
			))
		})

		ginkgo.It("reports proxy boolean assertions that hide the semantic value", func() {
			h := newRuleHarnessWithFiles("/repo/internal/projectdaemon/agent_presence_test.go", "github.com/perber/wiki/internal/projectdaemon", map[string]string{
				"/repo/internal/projectdaemon/frontmatter.go": `package projectdaemon

type Frontmatter struct{}

func ParseFrontmatter(raw string) (Frontmatter, string, bool, error) { return Frontmatter{}, "", true, nil }
`,
				"/repo/internal/projectdaemon/agent_presence_test.go": `package projectdaemon

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }
func Equal(actual any) any { return nil }
func HaveField(name string, matcher any) any { return nil }

func normalize() (string, bool) { return "", true }
func foundState(found bool) string {
	if found {
		return "found"
	}
	return "absent"
}
func boolState(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
type daemonState string
const (
	daemonReady daemonState = "ready"
	daemonStopped daemonState = "stopped"
)
func isDaemonReady(raw string) bool { return raw != "" }
func daemonReadyStateFor(raw string) daemonState {
	if isDaemonReady(raw) {
		return daemonReady
	}
	return daemonStopped
}
type routingScope string
const (
	routingHome routingScope = "home routing"
	routingWorkspace routingScope = "workspace routing"
)
func routingScopeFor(home bool) routingScope {
	if home {
		return routingHome
	}
	return routingWorkspace
}

func TestAgentPresence() {
	_, ok := normalize()
	Expect(ok).To(BeTrue())
	mcpCalled := false
	Expect(mcpCalled).To(BeFalse())
	cancelInvoked := false
	Expect(cancelInvoked).To(BeFalse())
	idleCanceled := false
	Expect(idleCanceled).To(BeFalse())
	Expect(struct {
		Event string
		Found bool
	}{Event: "start", Found: ok}).To(HaveField("Found", Equal(true)))
	state := foundState(ok)
	Expect(state).To(Equal("found"))
	grantOK := ok
	grantState := foundState(grantOK)
	Expect(grantState).To(Equal("found"))
	parsed := true
	parsedState := boolState(parsed)
	Expect(parsedState).To(Equal("true"))
	Expect(daemonReadyStateFor("raw")).To(Equal(daemonReady))
	Expect(routingScopeFor(ok)).To(Equal(routingHome))

	agentEnabled := true
	Expect(agentEnabled).To(BeTrue())
	Expect(struct{ Enabled bool }{Enabled: agentEnabled}).To(HaveField("Enabled", Equal(true)))
	contentChanged := true
	Expect(contentChanged).To(BeTrue())
	revisionCreated := false
	Expect(revisionCreated).To(BeFalse())
	linkRemoved := true
	Expect(linkRemoved).To(BeTrue())
	fileRenamed := true
	Expect(fileRenamed).To(BeTrue())
	userSetInContext := true
	Expect(userSetInContext).To(BeTrue())
	fm, body, has, err := ParseFrontmatter("raw")
	_ = fm
	_ = body
	_ = err
	Expect(has).To(BeTrue())
}
`,
			})
			calls := append(h.findCalls("To"), h.findCalls("foundState")...)
			calls = append(calls, h.findCalls("boolState")...)
			calls = append(calls, h.findCalls("daemonReadyStateFor")...)
			calls = append(calls, h.findCalls("routingScopeFor")...)
			for _, call := range calls {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
				"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
				"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
				"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
				"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
				"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
			))
		})

		ginkgo.It("reports matcher factories that gate error semantics on captured boolean state", func() {
			h := newRuleHarness("/repo/cmd/leafwiki/main_edges_gomega_test.go", "github.com/perber/wiki/cmd/leafwiki", `package main

import (
	"context"
	"errors"
	"net/http"
)

type GomegaMatcher interface{}
type matcherBuilder struct{}
type gcustomPackage struct{}
type workspaceRecord struct {
	ID string
}
type descriptor struct {
	SchemaVersion int
}

var gcustom gcustomPackage

func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }
func Satisfy(fn any) GomegaMatcher { return nil }

func MatchProjectDaemonHealthError(healthy bool, target error) GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return !healthy && errors.Is(err, target), nil
	}).WithMessage("match project daemon health error")
}

func matchLeafwikiHelperStopError(ready bool) GomegaMatcher {
	return Satisfy(func(err error) bool {
		return err == nil || errors.Is(err, context.Canceled) || ready
	})
}

func MatchHomeFederatedFirstContact(home bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(workspace workspaceRecord) (bool, error) {
		return home && workspace.ID != "", nil
	}).WithMessage("match home federated workspace")
}

func MatchStaleProjectDaemonHealth(healthy bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(desc *descriptor) (bool, error) {
		return desc != nil && desc.SchemaVersion == 0 && !healthy, nil
	}).WithMessage("match stale project daemon health")
}

func matchIssuedCSRFCookie(secure bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(cookie http.Cookie) (bool, error) {
		return cookie.Secure == secure, nil
	}).WithMessage("match CSRF cookie security")
}
`)
			for _, name := range []string{"MatchProjectDaemonHealthError", "matchLeafwikiHelperStopError", "MatchHomeFederatedFirstContact", "MatchStaleProjectDaemonHealth", "matchIssuedCSRFCookie"} {
				checkGomegaMatcherFactorySignature(h.ctx, h.findFunc(name))
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
				"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
				"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
				"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
			))
		})

		ginkgo.It("reports type-asserted map index assertion endpoints", func() {
			h := newRuleHarness("/repo/internal/http/router_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func ContainSubstring(needle string) any { return nil }

func TestRouterResponse() {
	resp := map[string]any{"content": "Root README", "status": 200}
	Expect(resp["content"].(string)).To(ContainSubstring("Root README"))
	Expect(resp["status"].(int)).To(Equal(200))
}
`)
			for _, call := range h.findCalls("To") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
				"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
			))
		})

		ginkgo.It("reports raw MCP protocol result status assertions", func() {
			h := newRuleHarness("/repo/internal/wiki/mcp/tools_test.go", "github.com/perber/wiki/internal/wiki/mcp", `package mcp

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }
func HaveField(name string, matcher any) any { return nil }
type Fields map[string]any
func MatchFields(_ any, fields Fields) any { return nil }
func SatisfyAll(matchers ...any) any { return nil }

type CallToolResult struct {
	IsError bool
}

func TestToolResult() {
	result := &CallToolResult{}
	Expect(result.IsError).To(BeFalse())
	Expect(result).To(HaveField("IsError", BeTrue()))
	Expect(result).To(SatisfyAll(HaveField("IsError", BeFalse())))
	Expect(result).To(MatchFields(nil, Fields{"IsError": BeFalse()}))
}
`)
			for _, call := range h.findCalls("To") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
				"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
				"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
				"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
			))
		})

		ginkgo.It("reports map index aliases asserted as local values", func() {
			h := newRuleHarness("/repo/internal/http/router_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func ContainSubstring(needle string) any { return nil }

func TestRouterResponse() {
	resp := map[string]any{"content": "Root README", "prefix": "/docs"}
	got := resp["prefix"]
	Expect(got).To(Equal("/docs"))
	content, _ := resp["content"].(string)
	Expect(content).To(ContainSubstring("Root"))
}
`)
			for _, call := range h.findCalls("To") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
				"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
			))
		})

		ginkgo.It("reports weak non-empty assertions on collections and semantic scalar fields", func() {
			h := newRuleHarness("/repo/internal/links/link_refactor_test.go", "github.com/perber/wiki/internal/links", `package links

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) NotTo(matcher any, extra ...any) {}
func BeEmpty() any { return nil }
func Not(matcher any) any { return nil }

func TestLinkWarnings() {
	warnings := []string{"reference link skipped"}
	Expect(warnings).NotTo(BeEmpty())
	byPath := map[string]int{"/docs": 1}
	Expect(byPath).To(Not(BeEmpty()))
	contextOutput := struct{ ContextToken string }{}
	userRecord := struct{ User struct{ ID string } }{}
	ordinary := struct{ Title string }{}
	Expect(contextOutput.ContextToken).NotTo(BeEmpty())
	Expect(userRecord.User.ID).To(Not(BeEmpty()))
	Expect(ordinary.Title).NotTo(BeEmpty())
}
`)
			for _, call := range append(h.findCalls("To"), h.findCalls("NotTo")...) {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.non-empty-collection: assert collection contents or cardinality semantics instead of only NotTo(BeEmpty())",
				"semh:gomega.non-empty-collection: assert collection contents or cardinality semantics instead of only NotTo(BeEmpty())",
				"semh:gomega.semantic-scalar-not-empty: assert semantic scalar value meaning instead of only checking for non-empty text",
				"semh:gomega.semantic-scalar-not-empty: assert semantic scalar value meaning instead of only checking for non-empty text",
			))
		})

		ginkgo.It("reports discarded semantic boolean returns in specs", func() {
			h := newRuleHarnessWithFiles("/repo/internal/agenthooks/agenthooks_test.go", "github.com/perber/wiki/internal/agenthooks", map[string]string{
				"/repo/internal/agenthooks/agenthooks.go": `package agenthooks

type ProviderID string
type Event struct{}

const ProviderCodex ProviderID = "codex"

func Normalize(provider ProviderID, raw []byte) (Event, bool) {
	return Event{}, true
}
`,
				"/repo/internal/agenthooks/agenthooks_test.go": `package agenthooks

type RoleHealth struct {
	PID int
}

func findRoleHealth() (RoleHealth, bool) {
	return RoleHealth{PID: 123}, true
}

func TestAgentHookNormalization() {
	_, _ = Normalize(ProviderCodex, []byte("{}"))
	initial, _ := findRoleHealth()
	_ = initial
}
`,
			})
			ast.Inspect(h.file, func(node ast.Node) bool {
				assign, ok := node.(*ast.AssignStmt)
				if ok {
					checkGomegaIgnoredSemanticBoolean(h.ctx, assign)
				}
				return true
			})

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.ignored-semantic-boolean: assert the semantic presence/status result instead of discarding a semantic boolean return with _",
				"semh:gomega.ignored-semantic-boolean: assert the semantic presence/status result instead of discarding a semantic boolean return with _",
			))
		})

		ginkgo.It("reports project daemon control status predicate assertions", func() {
			h := newRuleHarness("/repo/internal/projectdaemon/server_test.go", "github.com/perber/wiki/internal/projectdaemon", `package projectdaemon

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }

func IsControlStatus(err error, status int) bool { return true }

func TestControlStatus(err error) {
	Expect(IsControlStatus(err, 409)).To(BeTrue())
}
`)
			for _, call := range h.findCalls("To") {
				checkGomegaSemanticMatcher(h.ctx, call)
			}

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.control-status-matcher: assert project daemon control errors with MatchError or a domain matcher instead of IsControlStatus with boolean matchers",
			))
		})
	})

	ginkgo.Describe("waiver comments", func() {
		ginkgo.It("parses a valid rule-specific waiver with an explanation", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it -- documents the package-level invariant
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(diagnostics).To(BeEmpty())
			Expect(waivers).To(ConsistOf(SatisfyAll(
				WithTransform(func(waiver parsedWaiver) ruleID { return waiver.rule }, Equal(ruleGinkgoTopLevelIt)),
				WithTransform(func(waiver parsedWaiver) string { return waiver.explanation }, Not(BeEmpty())),
				WithTransform(func(waiver parsedWaiver) int {
					return h.ctx.pass.Fset.Position(waiver.pos).Line
				}, Equal(3)),
			)))
		})

		ginkgo.It("reports a waiver that omits the required explanation delimiter", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.top-level-it
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMissingExplanation)),
			))
		})

		ginkgo.It("reports a waiver for an unknown rule ID", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow ginkgo.made-up -- documents the package-level invariant
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverUnknownRule)),
			))
		})

		ginkgo.It("reports a malformed waiver without a rule ID", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow -- documents the package-level invariant
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
			))
		})

		ginkgo.It("reports a bare semh allow directive as malformed", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allow
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
			))
		})

		ginkgo.It("reports a glued semh allow directive as malformed", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// semh:allowginkgo.top-level-it -- documents the package-level invariant
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
			))
		})

		ginkgo.It("reports semh directives that are not standalone comments as malformed", func() {
			h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p

// TODO: revisit this exceptional shape semh:allow ginkgo.top-level-it -- package invariant
func TestPage() {}
`)

			waivers, diagnostics := h.ctx.collectWaivers()

			Expect(waivers).To(BeEmpty())
			Expect(diagnostics).To(ConsistOf(
				WithTransform(func(diagnostic semanticDiagnostic) ruleID { return diagnostic.rule }, Equal(ruleWaiverMalformed)),
			))
		})
	})

	ginkgo.Describe("policy helper branches", func() {
		ginkgo.It("classifies semantic and primitive type fallback cases", func() {
			name, hasSemanticType := semanticTypeNameOf(nil)
			Expect(hasSemanticType).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, hasSemanticType = semanticTypeNameOf(types.Typ[types.String])
			Expect(hasSemanticType).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, hasSemanticType = semanticTypeNameOf(namedStringType("PlainID"))
			Expect(hasSemanticType).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, hasSemanticType = semanticTypeNameOf(types.NewPointer(namedStringType("WorkspaceID")))
			Expect(hasSemanticType).To(BeTrue())
			Expect(name).To(Equal("WorkspaceID"))

			Expect(isString(nil)).To(BeFalse())
			Expect(isString(types.Typ[types.UntypedString])).To(BeTrue())
			Expect(isString(types.Typ[types.Int])).To(BeFalse())

			primitive, hasPrimitiveCarrier := primitiveCarrierTypeName(nil)
			Expect(hasPrimitiveCarrier).To(BeFalse())
			Expect(primitive).To(BeEmpty())
			primitive, hasPrimitiveCarrier = primitiveCarrierTypeName(types.NewStruct(nil, nil))
			Expect(hasPrimitiveCarrier).To(BeFalse())
			Expect(primitive).To(BeEmpty())
			primitive, hasPrimitiveCarrier = primitiveCarrierTypeName(types.Typ[types.Uint32])
			Expect(hasPrimitiveCarrier).To(BeTrue())
			Expect(primitive).To(Equal("uint32"))
			primitive, hasPrimitiveCarrier = primitiveCarrierTypeName(types.Typ[types.Float64])
			Expect(hasPrimitiveCarrier).To(BeFalse())
			Expect(primitive).To(BeEmpty())

			Expect(semanticConstructorAllowsSource("", "ErrorCode")).To(BeFalse())
			Expect(semanticConstructorAllowsSource("MessageID", "ErrorCode")).To(BeTrue())
			Expect(semanticConstructorAllowsSource("MessageID", "UserID")).To(BeFalse())
			Expect(semanticConstructorAllowsSource("ToolDescriptionID", "ToolID")).To(BeTrue())
		})

		ginkgo.It("classifies AST names and parent fallback cases", func() {
			target := &ast.Ident{Name: "target"}
			Expect(exprName(&ast.SelectorExpr{Sel: &ast.Ident{Name: "Field"}})).To(Equal("Field"))
			Expect(exprName(&ast.StarExpr{X: target})).To(Equal("target"))
			Expect(exprName(&ast.IndexExpr{X: target})).To(Equal("target"))
			Expect(callName(&ast.CallExpr{Fun: &ast.Ident{Name: "Do"}})).To(Equal("Do"))
			Expect(callName(&ast.CallExpr{Fun: &ast.SelectorExpr{Sel: &ast.Ident{Name: "Do"}}})).To(Equal("Do"))

			ctx := &analysisContext{parents: map[ast.Node]ast.Node{}}
			Expect(enclosingFuncName(ctx, target)).To(BeEmpty())
			Expect(enclosingFunc(ctx, target)).To(BeNil())
			Expect(isConstOrTypeDefinition(ctx, target)).To(BeFalse())

			fn := &ast.FuncDecl{Name: &ast.Ident{Name: "Run"}}
			ctx.parents[target] = fn
			Expect(enclosingFuncName(ctx, target)).To(Equal("Run"))
			Expect(enclosingFunc(ctx, target)).To(Equal(fn))
			Expect(isConstOrTypeDefinition(ctx, target)).To(BeFalse())
		})

		ginkgo.It("classifies AST struct and composite helper fallback cases", func() {
			strct := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
				{Names: []*ast.Ident{nil}},
				{Names: []*ast.Ident{{Name: "StatusCode"}}},
			}}}
			Expect(astStructHasField(strct, "messageID")).To(BeFalse())
			Expect(astStructHasField(&ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
				{Names: []*ast.Ident{{Name: "MessageID"}}},
			}}}, "messageID")).To(BeTrue())
			Expect(astStructHasContractSignal(strct)).To(BeTrue())
			Expect(astStructIsMessageBearing("Plain", strct)).To(BeTrue())

			target := &ast.Ident{Name: "target"}
			fn := &ast.FuncDecl{Name: &ast.Ident{Name: "Stop"}}
			ctx := &analysisContext{parents: map[ast.Node]ast.Node{target: fn}}
			named, typedStruct, lit, hasCompositeContext := enclosingNamedCompositeStructLiteral(ctx, target)
			Expect(hasCompositeContext).To(BeFalse())
			Expect(named).To(BeNil())
			Expect(typedStruct).To(BeNil())
			Expect(lit).To(BeNil())

			ctx.parents = map[ast.Node]ast.Node{}
			_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, target)
			Expect(hasCompositeContext).To(BeFalse())

			litNode := &ast.CompositeLit{}
			ctx.pass = &analysis.Pass{TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
				litNode: {Type: types.NewStruct(nil, nil)},
			}}}
			_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(hasCompositeContext).To(BeFalse())

			namedBasic := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "NamedBasic", nil), types.Typ[types.String], nil)
			ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: namedBasic}
			_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(hasCompositeContext).To(BeFalse())

			namedStruct := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "Payload", nil), types.NewStruct(nil, nil), nil)
			ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: types.NewPointer(namedStruct)}
			gotNamed, gotStruct, gotLit, hasCompositeContext := enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(hasCompositeContext).To(BeTrue())
			Expect(gotNamed.Obj().Name()).To(Equal("Payload"))
			Expect(gotStruct.NumFields()).To(BeZero())
			Expect(gotLit).To(Equal(litNode))
			wrappedNamed, wrappedStruct, hasCompositeContext := enclosingNamedCompositeStruct(ctx, litNode)
			Expect(hasCompositeContext).To(BeTrue())
			Expect(wrappedNamed).To(Equal(gotNamed))
			Expect(wrappedStruct).To(Equal(gotStruct))
		})

		ginkgo.DescribeTable("classifies remaining edge adapter filename cases",
			func(filename string) {
				Expect(isEdgeAdapterFile(filename)).To(BeTrue())
			},
			ginkgo.Entry("tree migration adapter", "/repo/internal/core/tree/migration_adapter.go"),
			ginkgo.Entry("workspacesync watcher adapter", "/repo/internal/workspacesync/watcher_adapter.go"),
		)

		ginkgo.DescribeTable("classifies owner adapter files across packages",
			func(packagePath string, filename string, want bool) {
				h := newRuleHarness(filename, packagePath, `package p`)
				Expect(isSemanticOwnerAdapterFile(h.ctx, h.file.Package)).To(Equal(want))
			},
			ginkgo.Entry("auth semantic owner", "github.com/perber/wiki/internal/core/auth", "/repo/internal/core/auth/semantic_types.go", true),
			ginkgo.Entry("revision semantic owner", "github.com/perber/wiki/internal/core/revision", "/repo/internal/core/revision/semantic_types.go", true),
			ginkgo.Entry("markdown validation semantic owner", "github.com/perber/wiki/internal/core/markdownvalidation", "/repo/internal/core/markdownvalidation/issue_codes.go", true),
			ginkgo.Entry("workspacesync semantic owner", "github.com/perber/wiki/internal/workspacesync", "/repo/internal/workspacesync/semantic_types.go", true),
			ginkgo.Entry("workspaceid semantic owner", "github.com/perber/wiki/internal/workspaceid", "/repo/internal/workspaceid/validate.go", true),
			ginkgo.Entry("ordinary package", "github.com/perber/wiki/internal/wiki/pages", "/repo/internal/wiki/pages/page.go", false),
		)

		ginkgo.It("permits direct casts only in policy allow contexts", func() {
			source := `package p
type PageID string
func NewFixturePageID(raw string) PageID { return PageID(raw) }
func SetMetadata(raw string) { _ = PageID(raw) }
func normal(raw string) PageID { return PageID(raw) }
const rawPageID = PageID("page-1")
`

			generated := newRuleHarness("/repo/vendor/example/p.go", "example.com/p", source)
			Expect(isAllowedDirectCastContext(generated.ctx, generated.findCall("PageID"), "PageID")).To(BeTrue())

			testLiteral := newRuleHarness("/repo/internal/p/page_test.go", "example.com/p", `package p
type PageID string
func literal() PageID { return PageID("page-1") }
`)
			Expect(isAllowedDirectCastContext(testLiteral.ctx, testLiteral.findCall("PageID"), "PageID")).To(BeTrue())

			fixture := newRuleHarness("/repo/internal/p/page_test.go", "example.com/p", source)
			Expect(isAllowedDirectCastContext(fixture.ctx, fixture.findCall("PageID"), "PageID")).To(BeTrue())

			edge := newRuleHarness("/repo/internal/wiki/import_adapter.go", "example.com/p", source)
			Expect(isAllowedDirectCastContext(edge.ctx, edge.findCalls("PageID")[1], "PageID")).To(BeTrue())

			normal := newRuleHarness("/repo/internal/p/page.go", "example.com/p", source)
			normalCasts := normal.findCalls("PageID")
			Expect(isAllowedDirectCastContext(normal.ctx, normalCasts[0], "PageID")).To(BeFalse())
			Expect(isConstOrTypeDefinition(normal.ctx, normalCasts[0])).To(BeFalse())
			Expect(isAllowedDirectCastContext(normal.ctx, normalCasts[len(normalCasts)-1], "PageID")).To(BeTrue())
			Expect(isConstOrTypeDefinition(normal.ctx, normalCasts[len(normalCasts)-1])).To(BeTrue())
		})

		ginkgo.It("recognizes type containment and semantic function contexts", func() {
			h := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func NewPageIDUnchecked(raw string) PageID { return PageID(raw) }
func (id PageID) Value() string { return string(id) }
func Scan(id PageID) string { return string(id) }
func noResult(raw string) { _ = raw }
`)
			constructor := h.findFunc("NewPageIDUnchecked")
			value := h.findFunc("Value")
			scan := h.findFunc("Scan")
			noResult := h.findFunc("noResult")

			Expect(functionReturnsSemanticType(h.ctx, noResult, "PageID")).To(BeFalse())
			Expect(functionReturnsSemanticType(h.ctx, constructor, "PageID")).To(BeTrue())
			Expect(functionHasSemanticReceiver(h.ctx, constructor, "PageID")).To(BeFalse())
			Expect(functionHasSemanticReceiver(h.ctx, value, "PageID")).To(BeTrue())
			Expect(functionHasSemanticParameter(h.ctx, noResult, "PageID")).To(BeFalse())
			Expect(functionHasSemanticParameter(h.ctx, scan, "PageID")).To(BeTrue())
			Expect(isAllowedSemanticOwnerAdapterFunc(h.ctx, h.findCall("string"), "PageID")).To(BeTrue())

			Expect(typeContainsSemanticType(nil, "PageID")).To(BeFalse())
			Expect(typeContainsSemanticType(types.NewSlice(namedStringType("PageID")), "PageID")).To(BeTrue())
			Expect(typeContainsSemanticType(types.NewArray(namedStringType("PageID"), 2), "PageID")).To(BeTrue())
			Expect(typeContainsSemanticType(types.Typ[types.String], "PageID")).To(BeFalse())

			emptyParams := &ast.FuncDecl{Name: &ast.Ident{Name: "NoParams"}, Type: &ast.FuncType{}}
			Expect(functionHasSemanticParameter(h.ctx, emptyParams, "PageID")).To(BeFalse())

			plain := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func Plain(raw string) string { return raw }
`)
			Expect(isAllowedSemanticOwnerAdapterFunc(plain.ctx, plain.findFunc("Plain"), "PageID")).To(BeFalse())
		})

		ginkgo.It("classifies wire persistence and JSON composite contexts", func() {
			wire := newRuleHarness("/repo/internal/service/page.go", "example.com/p", "package p\n"+
				"type PageResponse struct {\n"+
				"  PageID string `json:\"pageId\"`\n"+
				"  Plain string\n"+
				"}\n"+
				"func build(pageID string) PageResponse { return PageResponse{PageID: pageID, Plain: pageID} }\n")
			Expect(inJSONCompositeLiteral(wire.ctx, wire.findKeyValue("PageID").Value)).To(BeTrue())
			Expect(inJSONCompositeLiteral(wire.ctx, wire.findKeyValue("Plain").Value)).To(BeFalse())
			Expect(compositeFieldHasWireTag(wire.ctx, wire.firstCompositeLiteral(), "Missing")).To(BeFalse())
			Expect(isAllowedWireComposite(wire.ctx, wire.firstCompositeLiteral(), "PageResponse", wire.file.Package)).To(BeTrue())

			store := newRuleHarness("/repo/internal/wiki/page_store.go", "example.com/p", `package p
type pageRecord struct { PageID string }
type pageModel struct { PageID string }
func build(pageID string) pageRecord { return pageRecord{PageID: pageID} }
`)
			Expect(isPersistenceRowStruct(store.ctx, store.findTypeSpec("pageRecord"))).To(BeTrue())
			Expect(isPersistenceRowStruct(store.ctx, store.findTypeSpec("pageModel"))).To(BeFalse())
			Expect(isAllowedPersistenceRowKeyValue(store.ctx, store.findKeyValue("PageID").Value)).To(BeTrue())
			Expect(isPersistenceRowComposite(store.ctx, store.firstCompositeLiteral())).To(BeTrue())

			nonStore := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type pageRecord struct { PageID string }
func build(pageID string) pageRecord { return pageRecord{PageID: pageID} }
`)
			Expect(isPersistenceRowComposite(nonStore.ctx, nonStore.firstCompositeLiteral())).To(BeFalse())

			pointerStore := newRuleHarness("/repo/internal/wiki/page_store.go", "example.com/p", `package p
type pageRecord struct { PageID string }
func build(pageID string) *pageRecord { return &pageRecord{PageID: pageID} }
`)
			Expect(isPersistenceRowComposite(pointerStore.ctx, pointerStore.firstCompositeLiteral())).To(BeTrue())

			fset := token.NewFileSet()
			file := fset.AddFile("/repo/internal/wiki/page_store.go", -1, 100)
			pos := file.Pos(1)
			recordType := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "pageRecord", nil), types.NewStruct(nil, nil), nil)
			pointerComposite := &ast.CompositeLit{Type: &ast.Ident{Name: "pageRecord", NamePos: pos}}
			pointerCtx := &analysisContext{
				pass: &analysis.Pass{Fset: fset, TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
					pointerComposite: {Type: types.NewPointer(recordType)},
				}}},
			}
			Expect(isPersistenceRowComposite(pointerCtx, pointerComposite)).To(BeTrue())

			orphan := &ast.Ident{Name: "orphan"}
			funcBarrier := &ast.FuncDecl{Name: &ast.Ident{Name: "stop"}}
			manualCtx := &analysisContext{parents: map[ast.Node]ast.Node{orphan: funcBarrier}}
			Expect(inJSONCompositeLiteral(manualCtx, orphan)).To(BeFalse())
			manualCtx.parents = map[ast.Node]ast.Node{}
			Expect(inJSONCompositeLiteral(manualCtx, orphan)).To(BeFalse())
			Expect(isAllowedPersistenceRowKeyValue(manualCtx, orphan)).To(BeFalse())
			manualCtx.parents[orphan] = funcBarrier
			Expect(isAllowedPersistenceRowKeyValue(manualCtx, orphan)).To(BeFalse())

			pageIDField := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "PageID", types.Typ[types.String])
			taggedStruct := types.NewStruct([]*types.Var{pageIDField}, []string{`json:"pageId"`})
			pageResponse := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "PageResponse", nil), taggedStruct, nil)
			composite := &ast.CompositeLit{}
			manualCtx.pass = &analysis.Pass{Fset: token.NewFileSet(), TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
				composite: {Type: types.NewPointer(pageResponse)},
			}}}
			Expect(compositeFieldHasWireTag(manualCtx, composite, "PageID")).To(BeTrue())

			manualCtx.pass.TypesInfo.Types[composite] = types.TypeAndValue{Type: types.NewStruct(nil, nil)}
			Expect(compositeFieldHasWireTag(manualCtx, composite, "PageID")).To(BeFalse())
			manualCtx.pass.TypesInfo.Types[composite] = types.TypeAndValue{Type: namedStringType("PageResponse")}
			Expect(compositeFieldHasWireTag(manualCtx, composite, "PageID")).To(BeFalse())
		})

		ginkgo.It("distinguishes stable and localized literal contexts", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
func respondError(code string) {}
func validationMessage() string {
	respondError("page_not_found")
	return "field_required"
}
func plain() string { return "hello world" }
`)
			stableCall := h.findLiteral("page_not_found")
			stableReturn := h.findLiteral("field_required")
			plain := h.findLiteral("hello world")

			Expect(isStableContractLiteral(h.ctx, stableCall, "page_not_found")).To(BeTrue())
			Expect(stableLiteralContextSuggestsContract(h.ctx, stableReturn)).To(BeTrue())
			Expect(stableLiteralContextSuggestsContract(h.ctx, plain)).To(BeFalse())
			Expect(isStableMessageLikeLiteral("validation.page.title_required")).To(BeTrue())
			Expect(isStableMessageLikeLiteral("wiki_get_page")).To(BeTrue())
			Expect(isStableMessageLikeLiteral("page_not_found")).To(BeTrue())
			Expect(isStableMessageLikeLiteral("plain prose")).To(BeFalse())

			Expect(isStableLiteralAllowed(h.ctx, stableCall)).To(BeFalse())
			Expect(isLocalizedProseLiteralAllowed(h.ctx, plain)).To(BeFalse())
			testFile := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func plain() string { return "hello world" }
`)
			Expect(isStableLiteralAllowed(testFile.ctx, testFile.findLiteral("hello world"))).To(BeTrue())
			Expect(isLocalizedProseLiteralAllowed(testFile.ctx, testFile.findLiteral("hello world"))).To(BeTrue())

			plainCall := &ast.CallExpr{Args: []ast.Expr{
				&ast.Ident{Name: "notLiteral"},
				&ast.BasicLit{Kind: token.INT, Value: "1"},
				&ast.BasicLit{Kind: token.STRING, Value: `"validation.page.required"`},
			}}
			Expect(localizedProseConstructorRequiresCatalogOnly("OtherConstructor", h.ctx, plainCall)).To(BeTrue())
			Expect(callHasRawStableContractArg(h.ctx, &ast.CallExpr{})).To(BeFalse())
			Expect(callContainsArg(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}}}, stableCall)).To(BeFalse())

			kv := &ast.KeyValueExpr{Key: &ast.Ident{Name: "errorCode"}}
			matches, terminal := stableLiteralContextDecision(h.ctx, kv, stableCall)
			Expect(matches).To(BeTrue())
			Expect(terminal).To(BeFalse())
			assign := &ast.AssignStmt{Lhs: []ast.Expr{&ast.Ident{Name: "errorCode"}}, Rhs: []ast.Expr{stableCall}}
			matches, terminal = stableLiteralContextDecision(h.ctx, assign, stableCall)
			Expect(matches).To(BeTrue())
			Expect(terminal).To(BeFalse())
			spec := &ast.ValueSpec{Names: []*ast.Ident{{Name: "errorCode"}}, Values: []ast.Expr{stableCall}}
			matches, terminal = stableLiteralContextDecision(h.ctx, spec, stableCall)
			Expect(matches).To(BeTrue())
			Expect(terminal).To(BeFalse())
			Expect(stableLiteralContextSuggestsContract(&analysisContext{parents: map[ast.Node]ast.Node{}}, stableCall)).To(BeFalse())

			Expect(isCLIOutputWriterExpr(&ast.Ident{Name: "stdout"})).To(BeFalse())
			Expect(isResponsePayloadLiteral(&analysisContext{parents: map[ast.Node]ast.Node{plain: &ast.FuncDecl{}}}, plain)).To(BeFalse())
			Expect(isResponsePayloadLiteral(&analysisContext{parents: map[ast.Node]ast.Node{}}, plain)).To(BeFalse())
			Expect(isStrictLocalizedProseContractLiteral(&analysisContext{parents: map[ast.Node]ast.Node{}}, plain, "plain prose")).To(BeFalse())

			pkgName, calleeName := calleePackageAndName(h.ctx, &ast.CallExpr{Fun: &ast.SelectorExpr{Sel: &ast.Ident{Name: "Method"}}})
			Expect(pkgName).To(BeEmpty())
			Expect(calleeName).To(Equal("Method"))
			pkgName, calleeName = calleePackageAndName(h.ctx, &ast.CallExpr{Fun: &ast.BasicLit{Kind: token.STRING}})
			Expect(pkgName).To(BeEmpty())
			Expect(calleeName).To(BeEmpty())
		})

		ginkgo.It("reports raw runtime role health wire literals in test fixtures", func() {
			h := newRuleHarness("/repo/internal/wiki/wiki_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type runtimeRoleHealthWireCheck struct {
	Key   string
	State string
}

var workspacedRoleCrashedHealth = runtimeRoleHealthWireCheck{
	Key:   "role_workspaced",
	State: "crashed",
}
`)
			checkStableLiteral(h.ctx, h.findLiteral("role_workspaced"))
			checkStableLiteral(h.ctx, h.findLiteral("crashed"))

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:contract.raw-literal: raw stable contract literal \"role_workspaced\" used in test assertion code; use the typed constant or semantic helper",
				"semh:contract.raw-literal: raw stable contract literal \"crashed\" used in test assertion code; use the typed constant or semantic helper",
			))
		})
	})

	ginkgo.Describe("rule branches", func() {
		ginkgo.It("reports LastError assertions that only prove non-empty rendered text", func() {
			h := newRuleHarness("/repo/internal/workspacesync/service_test.go", "github.com/perber/wiki/internal/workspacesync", `package workspacesync

type SyncStatus struct {
	LastError string
}

type assertion struct{}

func Expect(actual any) assertion { return assertion{} }
func (assertion) NotTo(matcher any, extras ...any) {}
func BeEmpty() any { return nil }

func TestSyncStatus() {
	status := SyncStatus{LastError: "writeback failed"}
	Expect(status.LastError).NotTo(BeEmpty())
}
`)

			checkGomegaSemanticMatcher(h.ctx, h.findCall("NotTo"))

			Expect(h.diagnosticMessages()).To(ConsistOf(
				"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
			))
		})

		ginkgo.It("ignores string leak and conversion early exit cases", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PlainID string
func (id PlainID) String() string { return string(id) }
func use(id PlainID, raw int) {
	_ = id.String()
	_ = string(raw)
}
`)
			checkStringLeak(h.ctx, h.findCall("String"))
			checkStringConversionLeak(h.ctx, h.findCall("string"))
			Expect(h.diagnostics).To(BeEmpty())

			allowed := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) { _ = id.String() }
`)
			checkStringLeak(allowed.ctx, allowed.findCall("String"))
			Expect(allowed.diagnostics).To(BeEmpty())
		})

		ginkgo.It("reports string assignment key-value and return diagnostics", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
type domainRecord struct { PageID string }
func use(id PageID, values map[string]string) string {
	values[id.String()] = "seen"
	var local string
	local = id.String()
	record := domainRecord{PageID: id.String()}
	_, _ = local, record
	return id.String()
}
`)
			for _, call := range h.findCalls("String") {
				if call.Pos() == h.findFunc("String").Body.List[0].(*ast.ReturnStmt).Results[0].Pos() {
					continue
				}
				checkStringLeak(h.ctx, call)
			}
			Expect(h.diagnosticMessages()).To(ContainElements(
				ContainSubstring("map index"),
				ContainSubstring("local local"),
				ContainSubstring("semantic field PageID"),
				ContainSubstring("returned as string"),
			))
		})

		ginkgo.It("reports string value specs and direct assignment helper edges", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value = id.String()
	_ = value
}
`)
			checkStringLeak(h.ctx, h.findCall("String"))
			Expect(h.diagnosticMessages()).To(ContainElement(ContainSubstring("local value")))

			vendor := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value = id.String()
	_ = value
}
`)
			checkStringLeak(vendor.ctx, vendor.findCall("String"))
			Expect(vendor.diagnostics).To(BeEmpty())

			expr := &ast.Ident{Name: "expr"}
			ctx := h.ctx
			ctx.pass.Report = func(diagnostic analysis.Diagnostic) {
				h.diagnostics = append(h.diagnostics, diagnostic)
			}
			checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
				Lhs: []ast.Expr{&ast.Ident{Name: "local"}},
				Rhs: []ast.Expr{&ast.Ident{Name: "other"}},
			})
			checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
				Lhs: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"field"`}},
				Rhs: []ast.Expr{expr},
			})
			checkStringAssignment(ctx, expr, "PageID", &ast.AssignStmt{
				Lhs: []ast.Expr{&ast.IndexExpr{X: expr, Index: &ast.BasicLit{Kind: token.STRING, Value: `"key"`}}},
				Rhs: []ast.Expr{&ast.Ident{Name: "other"}},
			})
			checkStringKeyValue(ctx, expr, "PageID", &ast.KeyValueExpr{
				Key:   &ast.BasicLit{Kind: token.STRING, Value: `"`},
				Value: expr,
			})
			checkStringValueSpec(vendor.ctx, vendor.findCall("String"), "PageID", &ast.ValueSpec{})
			checkStringValueSpec(ctx, expr, "PageID", &ast.ValueSpec{
				Values: []ast.Expr{&ast.Ident{Name: "other"}, expr},
				Names:  []*ast.Ident{{Name: "first"}},
			})
			checkStringAssignment(vendor.ctx, vendor.findCall("String"), "PageID", &ast.AssignStmt{})
			Expect(h.diagnosticMessages()).To(ContainElement(ContainSubstring("map index")))

			vendorAssignment := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	var value string
	value = id.String()
	_ = value
}
`)
			checkStringLeak(vendorAssignment.ctx, vendorAssignment.findCall("String"))
			Expect(vendorAssignment.diagnostics).To(BeEmpty())

			vendorReturn := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) string {
	return id.String()
}
`)
			checkStringReturn(vendorReturn.ctx, vendorReturn.findCall("String"), "PageID")
			Expect(vendorReturn.diagnostics).To(BeEmpty())

			ignoredExpression := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	id.String()
}
`)
			checkStringLeak(ignoredExpression.ctx, ignoredExpression.findCall("String"))
			Expect(ignoredExpression.diagnostics).To(BeEmpty())
		})

		ginkgo.It("recognizes terminal string call boundaries", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import (
	"fmt"
	"log"
	"log/slog"
	"net/url"
)
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) {
	fmt.Println(id.String())
	log.Printf("%s", id.String())
	slog.Info("page", "id", id.String())
	url.PathEscape(id.String())
}
`)
			for _, name := range []string{"Println", "Printf", "Info", "PathEscape"} {
				Expect(isAllowedTerminalStringCall(h.ctx, h.findCall(name))).To(BeTrue())
				Expect(isAllowedTerminalCallBoundary(h.ctx, h.findCall(name))).To(BeTrue())
			}

			gitHashBoundary := newRuleHarness("/repo/internal/workspacesync/gitrevisions/semantic_types.go", "github.com/perber/wiki/internal/workspacesync/gitrevisions", `package gitrevisions
type CommitHash string
type Hash struct{}
type plumbingPackage struct{}
func (plumbingPackage) NewHash(string) Hash { return Hash{} }
var plumbing plumbingPackage
func (hash CommitHash) String() string { return string(hash) }
func PlumbingHashFromCommitHash(hash CommitHash) Hash {
	return plumbing.NewHash(hash.String())
}
`)
			newHash := gitHashBoundary.findCall("NewHash").Fun.(*ast.SelectorExpr).Sel
			gitHashBoundary.ctx.pass.TypesInfo.Uses[newHash] = types.NewFunc(token.NoPos, types.NewPackage("github.com/go-git/go-git/v6/plumbing", "plumbing"), "NewHash", nil)
			checkStringLeak(gitHashBoundary.ctx, gitHashBoundary.findCall("String"))
			Expect(gitHashBoundary.diagnostics).To(BeEmpty())

			nested := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import "fmt"
type PageID string
func (id PageID) String() string { return string(id) }
func use(id PageID) string { return wrap(fmt.Sprint(id.String())) }
func wrap(value string) string { return value }
`)
			Expect(isAllowedTerminalStringCall(nested.ctx, nested.findCall("Sprint"))).To(BeFalse())
			checkStringLeak(nested.ctx, nested.findCall("String"))
			Expect(nested.diagnosticMessages()).To(ContainElement(ContainSubstring("before internal call Sprint")))

			otherTerminals := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
import (
	"log"
	"log/slog"
)
func use() {
	log.Output(1, "x")
	slog.SetDefault(nil)
}
`)
			Expect(isAllowedTerminalStringCall(otherTerminals.ctx, otherTerminals.findCall("Output"))).To(BeFalse())
			Expect(isAllowedTerminalStringCall(otherTerminals.ctx, otherTerminals.findCall("SetDefault"))).To(BeFalse())

			stmt := &ast.CallExpr{Fun: &ast.Ident{Name: "terminal"}}
			binary := &ast.BinaryExpr{Op: token.LSS}
			manualCtx := &analysisContext{parents: map[ast.Node]ast.Node{stmt: binary}}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())
			manualCtx.parents[stmt] = &ast.ParenExpr{}
			manualCtx.parents[manualCtx.parents[stmt]] = &ast.ExprStmt{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeTrue())
			manualCtx.parents = map[ast.Node]ast.Node{stmt: &ast.BinaryExpr{Op: token.ADD}}
			manualCtx.parents[manualCtx.parents[stmt]] = &ast.ReturnStmt{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeTrue())
			manualCtx.parents[stmt] = &ast.CallExpr{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())
			manualCtx.parents[stmt] = &ast.FuncDecl{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())
			manualCtx.parents[stmt] = &ast.IfStmt{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())
			manualCtx.pass = h.ctx.pass
			manualCtx.parents[stmt] = &ast.KeyValueExpr{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())
			manualCtx.parents = map[ast.Node]ast.Node{}
			Expect(isAllowedTerminalCallBoundary(manualCtx, stmt)).To(BeFalse())

			testCalls := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
import (
	"net/http"
	"path/filepath"
	"testing"
)
func TestCalls(t *testing.T) {
	t.Helper()
	t.Cleanup(func(){})
	_ = filepath.Join("a", "b")
	_, _ = http.NewRequest("GET", "/", nil)
	_ = append([]string{}, "x")
	assertEqual("x")
	requireEqual("x")
}
func assertEqual(value string) {}
func requireEqual(value string) {}
`)
			Expect(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("Join"))).To(BeTrue())
			Expect(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("NewRequest"))).To(BeTrue())
			Expect(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("append"))).To(BeTrue())
			Expect(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("assertEqual"))).To(BeTrue())
			Expect(isAllowedTestStringCall(testCalls.ctx, testCalls.findCall("requireEqual"))).To(BeTrue())
			Expect(isAllowedTestAssertionCall(testCalls.findCall("Cleanup"))).To(BeFalse())

			comparison := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func compare(id PageID, raw string) bool {
	return raw == id.String()
}
`)
			var binaryExpr *ast.BinaryExpr
			ast.Inspect(comparison.file, func(node ast.Node) bool {
				if binaryExpr != nil {
					return false
				}
				if expr, ok := node.(*ast.BinaryExpr); ok {
					binaryExpr = expr
					return false
				}
				return true
			})
			Expect(binaryExpr).NotTo(BeNil())
			Expect(isAllowedSerializedTestComparison(comparison.ctx, comparison.findCall("String"), binaryExpr)).To(BeFalse())
			Expect(isAllowedSerializedTestComparison(comparison.ctx, &ast.BasicLit{Kind: token.STRING, Value: `"other"`, ValuePos: comparison.file.Package}, binaryExpr)).To(BeFalse())

			transform := newRuleHarness("/repo/internal/analysis/semantichygiene/testdata/semanticcases/message_constructor.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/semanticcases", `package semanticcases
import "strings"
type ErrorCode string
type MessageID string
func MessageIDForCode(code ErrorCode) MessageID {
	_ = strings.Contains(string(code), "x")
	return ""
}
`)
			Expect(isAllowedSemanticConstructorTransform(transform.ctx, transform.findCall("Contains"), "ErrorCode")).To(BeFalse())

			adapterReturn := newRuleHarness("/repo/internal/wiki/import_adapter.go", "example.com/p", `package p
type PageID string
func (id PageID) String() string { return string(id) }
func Other(id PageID) string { return id.String() }
`)
			Expect(isAllowedAdapterStringReturn(adapterReturn.ctx, adapterReturn.findCall("String"))).To(BeFalse())
		})

		ginkgo.It("reports direct cast and unchecked constructor diagnostics", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func use(raw string, count int) {
	_ = PageID(count)
	_ = PageID(raw)
	_ = NewPageIDUnchecked(count)
	_ = NewPageIDUnchecked(raw)
}
`)
			pageIDCalls := h.findCalls("PageID")
			checkDirectCast(h.ctx, pageIDCalls[0])
			Expect(h.diagnostics).To(BeEmpty())

			h.resetDiagnostics()
			uncheckedCalls := h.findCalls("NewPageIDUnchecked")
			checkDirectCast(h.ctx, uncheckedCalls[0])
			Expect(h.diagnostics).To(BeEmpty())

			h.resetDiagnostics()
			checkUncheckedConstructorCall(h.ctx, uncheckedCalls[0])
			Expect(h.diagnosticMessages()).To(ContainElement(ContainSubstring("unchecked constructor NewPageIDUnchecked")))

			primitiveCall := &ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "plain"}}}
			Expect(callHasPrimitiveArg(h.ctx, primitiveCall)).To(BeFalse())

			generated := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func use(raw string) { _ = NewPageIDUnchecked(raw) }
`)
			Expect(isAllowedUncheckedConstructorCall(generated.ctx, generated.findCall("NewPageIDUnchecked"), "PageID")).To(BeTrue())

			fixture := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func NewFixturePageID(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
			Expect(isAllowedUncheckedConstructorCall(fixture.ctx, fixture.findCall("NewPageIDUnchecked"), "PageID")).To(BeTrue())

			owner := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func MakePageID(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
			Expect(isAllowedUncheckedConstructorOwnerContext(owner.ctx, owner.findCall("NewPageIDUnchecked"), "PageID")).To(BeTrue())

			semanticConstructor := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func NewPageIDUnchecked(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
			Expect(isAllowedUncheckedConstructorCall(semanticConstructor.ctx, semanticConstructor.findCall("NewPageIDUnchecked"), "PageID")).To(BeTrue())
		})

		ginkgo.It("reports message field passthrough and response status diagnostics", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type MessageID string
type ErrorCode string
type validationResult struct {
	MessageID MessageID
	Message string
	Code ErrorCode
}
type validationIssue struct {
	Message string
	Code ErrorCode
}
type response H
type H map[string]any
func build(message string, lastError string) H {
	_ = validationResult{MessageID: "validation.ok", Message: message, Code: "ok"}
	_ = validationIssue{Message: "plain", Code: "ok"}
	return H{"lastError": lastError}
}
func NewLocalizedError(code ErrorCode, message string) {}
func makeError(message string) { NewLocalizedError("ok", message) }
`)
			checkMessageFieldValue(h.ctx, h.findKeyValue("Message"))
			Expect(isMessageFieldValueFreeFormPassthrough(h.ctx, h.findKeyValue("Message"))).To(BeTrue())
			checkResponseStatusForward(h.ctx, h.findKeyValue("lastError"))
			checkMessagePassthroughCall(h.ctx, h.findCall("NewLocalizedError"))
			Expect(h.diagnosticMessages()).To(ContainElements(
				ContainSubstring("passes through free-form text despite MessageID"),
				ContainSubstring("forwards message-bearing status text"),
				ContainSubstring("localized error constructor NewLocalizedError"),
			))

			Expect(compositeLiteralHasKey(nil, "messageID")).To(BeFalse())
			Expect(callHasFreeFormMessageArg(h.ctx, h.findCall("NewLocalizedError"))).To(BeTrue())
			Expect(exprSuggestsMessageStatusForward(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "lastError"}}})).To(BeTrue())
			Expect(exprSuggestsMessageStatusForward(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}}})).To(BeFalse())
			Expect(exprSuggestsMessageStatusForward(&ast.BasicLit{Kind: token.STRING, Value: `"plain"`})).To(BeFalse())
			Expect(compositeLiteralHasKey(&ast.CompositeLit{Elts: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"plain"`}}}, "messageID")).To(BeFalse())

			outside := &ast.KeyValueExpr{Key: &ast.Ident{Name: "Message"}, Value: &ast.BasicLit{Kind: token.STRING, Value: `"plain"`}}
			checkMessageFieldValue(&analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}, outside)
			checkResponseStatusForward(h.ctx, &ast.KeyValueExpr{Key: &ast.BasicLit{Kind: token.STRING, Value: `"lastError"`}, Value: &ast.Ident{Name: "other"}})
			_, warningHasMessageID := warningFieldValueMissingMessageIDNamed(&analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}, outside)
			Expect(warningHasMessageID).To(BeFalse())

			Expect(exprIsFreeFormMessageParam(h.ctx, &ast.BasicLit{Kind: token.STRING, Value: `"message"`})).To(BeFalse())
			defsMessage := &ast.Ident{Name: "message"}
			defsVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
			defsCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
				Uses: map[*ast.Ident]types.Object{},
				Defs: map[*ast.Ident]types.Object{defsMessage: defsVar},
			}}, parents: map[ast.Node]ast.Node{}}
			Expect(exprIsFreeFormMessageParam(defsCtx, defsMessage)).To(BeFalse())
			nonStringMessage := &ast.Ident{Name: "message"}
			nonStringVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.Int])
			nonStringCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
				Uses: map[*ast.Ident]types.Object{nonStringMessage: nonStringVar},
			}}, parents: map[ast.Node]ast.Node{}}
			Expect(exprIsFreeFormMessageParam(nonStringCtx, nonStringMessage)).To(BeFalse())
			freeMessage := &ast.Ident{Name: "message"}
			freeVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
			freeCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
				Uses: map[*ast.Ident]types.Object{freeMessage: freeVar},
			}}, parents: map[ast.Node]ast.Node{}}
			Expect(exprIsFreeFormMessageParam(freeCtx, freeMessage)).To(BeFalse())

			otherMessage := &ast.Ident{Name: "message"}
			paramName := &ast.Ident{Name: "other"}
			param := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
			freeCtx.pass.TypesInfo.Defs = map[*ast.Ident]types.Object{paramName: types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "other", types.Typ[types.String])}
			freeCtx.pass.TypesInfo.Uses = map[*ast.Ident]types.Object{otherMessage: param}
			freeCtx.parents[otherMessage] = &ast.FuncDecl{Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{paramName}}}}}}
			Expect(exprIsFreeFormMessageParam(freeCtx, otherMessage)).To(BeFalse())
		})

		ginkgo.It("reports signature and validator helper diagnostics", func() {
			h := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type PageID string
type CommitHash string
type service interface {
	GetPage(id string)
	embedded
}
type embedded interface{}
func writeControlError(status int, message string) {}
func GetPage(id string, count uint, plain bool) {}
func ChangedMarkdownPaths(string, string) {}
func ValidatePageID(pageID string) (string, error) { return pageID, nil }
func ValidatePlain(raw string) (bool, error) { return true, nil }
`)
			checkSignature(h.ctx, h.findFunc("writeControlError"))
			checkSignature(h.ctx, h.findFunc("GetPage"))
			checkSignature(h.ctx, h.findFunc("ChangedMarkdownPaths"))
			checkValidatorReturn(h.ctx, h.findFunc("ValidatePageID"))
			checkValidatorReturn(h.ctx, h.findFunc("ValidatePlain"))
			checkTypeSpec(h.ctx, h.findTypeSpec("service"))
			Expect(h.diagnosticMessages()).To(ContainElements(
				ContainSubstring("localized prose sink writeControlError"),
				ContainSubstring("semantic-looking parameter id uses string"),
				ContainSubstring("semantic-looking parameter commitHash uses string"),
				ContainSubstring("validator ValidatePageID returns primitive string"),
			))

			Expect(semanticContextName(nil)).To(BeEmpty())
			paramName, typeName, hasSemanticType := semanticTypeForUnnamedParam("Other", "Other", 1)
			Expect(hasSemanticType).To(BeFalse())
			Expect(paramName).To(BeEmpty())
			Expect(typeName).To(BeEmpty())
			Expect(isRawStringCarrier(nil)).To(BeFalse())
			Expect(isRawStringCarrier(types.NewSlice(types.Typ[types.String]))).To(BeTrue())
			Expect(isRawStringCarrier(types.NewArray(types.Typ[types.String], 2))).To(BeTrue())
			Expect(isRawStringCarrier(types.NewMap(types.Typ[types.String], types.Typ[types.Int]))).To(BeTrue())
			Expect(isRawStringCarrier(types.NewMap(types.Typ[types.Int], types.Typ[types.String]))).To(BeFalse())

			checkSignatureParams(h.ctx, "NoParams", "NoParams", nil)
			nilNameField := &ast.Field{Names: []*ast.Ident{nil}, Type: ast.NewIdent("string")}
			h.ctx.pass.TypesInfo.Types[nilNameField.Type] = types.TypeAndValue{Type: types.Typ[types.String]}
			checkStringSignatureField(h.ctx, nilNameField, "GetPage", "GetPage", 0)
			primitiveNilNameField := &ast.Field{Names: []*ast.Ident{nil}, Type: ast.NewIdent("int")}
			h.ctx.pass.TypesInfo.Types[primitiveNilNameField.Type] = types.TypeAndValue{Type: types.Typ[types.Int]}
			checkPrimitiveSignatureField(h.ctx, primitiveNilNameField, "Search", "SearchPage")

			_, _, hasSemanticType = semanticTypeForUnnamedParam("Other", "Other", 0)
			Expect(hasSemanticType).To(BeFalse())

			testFile := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type PageID string
func ValidatePageID(pageID string) (string, error) { return pageID, nil }
`)
			checkValidatorReturn(testFile.ctx, testFile.findFunc("ValidatePageID"))
			Expect(testFile.diagnostics).To(BeEmpty())

			ifaceSpec := &ast.TypeSpec{
				Name: &ast.Ident{Name: "service"},
				Type: &ast.InterfaceType{Methods: &ast.FieldList{List: []*ast.Field{{
					Names: []*ast.Ident{nil},
					Type:  &ast.FuncType{},
				}}}},
			}
			checkInterfaceSignatures(h.ctx, ifaceSpec)

			structSpec := &ast.TypeSpec{
				Name: &ast.Ident{Name: "SearchRequest"},
				Type: &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
					{Names: []*ast.Ident{nil}, Type: ast.NewIdent("int")},
					{Names: []*ast.Ident{nil}, Type: ast.NewIdent("string")},
				}}},
			}
			fields := structSpec.Type.(*ast.StructType).Fields.List
			h.ctx.pass.TypesInfo.Types[fields[0].Type] = types.TypeAndValue{Type: types.Typ[types.Int]}
			h.ctx.pass.TypesInfo.Types[fields[1].Type] = types.TypeAndValue{Type: types.Typ[types.String]}
			checkStructFields(h.ctx, structSpec)

			wirePrimitive := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", "package p\n"+
				"type SearchRequest struct { Limit int `json:\"limit\"` }\n")
			checkStructFields(wirePrimitive.ctx, wirePrimitive.findTypeSpec("SearchRequest"))
		})
	})
})
