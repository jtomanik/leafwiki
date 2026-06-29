package semantichygiene

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
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

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	Expect(err).NotTo(HaveOccurred())

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
	pkg, err := config.Check(packagePath, fset, []*ast.File{file}, info)
	Expect(err).NotTo(HaveOccurred(), strings.Join(typeErrors, "\n"))

	harness := &ruleHarness{file: file}
	pass := &analysis.Pass{
		Fset:      fset,
		Files:     []*ast.File{file},
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
}

func (h *ruleHarness) diagnosticMessages() []string {
	ginkgo.GinkgoHelper()

	messages := make([]string, len(h.diagnostics))
	for i, diagnostic := range h.diagnostics {
		messages[i] = diagnostic.Message
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
	Expect(found).NotTo(BeEmpty(), "call %q should exist", name)
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

var _ = ginkgo.Describe("semantichygiene edge coverage", func() {
	ginkgo.Describe("policy helper branches", func() {
		ginkgo.It("covers semantic and primitive type fallbacks", func() {
			name, ok := semanticTypeNameOf(nil)
			Expect(ok).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, ok = semanticTypeNameOf(types.Typ[types.String])
			Expect(ok).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, ok = semanticTypeNameOf(namedStringType("PlainID"))
			Expect(ok).To(BeFalse())
			Expect(name).To(BeEmpty())
			name, ok = semanticTypeNameOf(types.NewPointer(namedStringType("WorkspaceID")))
			Expect(ok).To(BeTrue())
			Expect(name).To(Equal("WorkspaceID"))

			Expect(isString(nil)).To(BeFalse())
			Expect(isString(types.Typ[types.UntypedString])).To(BeTrue())
			Expect(isString(types.Typ[types.Int])).To(BeFalse())

			primitive, ok := primitiveCarrierTypeName(nil)
			Expect(ok).To(BeFalse())
			Expect(primitive).To(BeEmpty())
			primitive, ok = primitiveCarrierTypeName(types.NewStruct(nil, nil))
			Expect(ok).To(BeFalse())
			Expect(primitive).To(BeEmpty())
			primitive, ok = primitiveCarrierTypeName(types.Typ[types.Uint32])
			Expect(ok).To(BeTrue())
			Expect(primitive).To(Equal("uint32"))
			primitive, ok = primitiveCarrierTypeName(types.Typ[types.Float64])
			Expect(ok).To(BeFalse())
			Expect(primitive).To(BeEmpty())

			Expect(semanticConstructorAllowsSource("", "ErrorCode")).To(BeFalse())
			Expect(semanticConstructorAllowsSource("MessageID", "ErrorCode")).To(BeTrue())
			Expect(semanticConstructorAllowsSource("MessageID", "UserID")).To(BeFalse())
			Expect(semanticConstructorAllowsSource("ToolDescriptionID", "ToolID")).To(BeTrue())
		})

		ginkgo.It("covers AST name and parent fallbacks", func() {
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

		ginkgo.It("covers AST struct and composite helper fallbacks", func() {
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
			named, typedStruct, lit, ok := enclosingNamedCompositeStructLiteral(ctx, target)
			Expect(ok).To(BeFalse())
			Expect(named).To(BeNil())
			Expect(typedStruct).To(BeNil())
			Expect(lit).To(BeNil())

			ctx.parents = map[ast.Node]ast.Node{}
			_, _, _, ok = enclosingNamedCompositeStructLiteral(ctx, target)
			Expect(ok).To(BeFalse())

			litNode := &ast.CompositeLit{}
			ctx.pass = &analysis.Pass{TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
				litNode: {Type: types.NewStruct(nil, nil)},
			}}}
			_, _, _, ok = enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(ok).To(BeFalse())

			namedBasic := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "NamedBasic", nil), types.Typ[types.String], nil)
			ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: namedBasic}
			_, _, _, ok = enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(ok).To(BeFalse())

			namedStruct := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "Payload", nil), types.NewStruct(nil, nil), nil)
			ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: types.NewPointer(namedStruct)}
			gotNamed, gotStruct, gotLit, ok := enclosingNamedCompositeStructLiteral(ctx, litNode)
			Expect(ok).To(BeTrue())
			Expect(gotNamed.Obj().Name()).To(Equal("Payload"))
			Expect(gotStruct.NumFields()).To(BeZero())
			Expect(gotLit).To(Equal(litNode))
			wrappedNamed, wrappedStruct, ok := enclosingNamedCompositeStruct(ctx, litNode)
			Expect(ok).To(BeTrue())
			Expect(wrappedNamed).To(Equal(gotNamed))
			Expect(wrappedStruct).To(Equal(gotStruct))
		})

		ginkgo.DescribeTable("covers remaining edge adapter filename cases",
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

		ginkgo.It("covers direct-cast policy allow contexts", func() {
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

		ginkgo.It("covers type containment and semantic function context helpers", func() {
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

		ginkgo.It("covers wire, persistence, and JSON composite helpers", func() {
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

		ginkgo.It("covers stable and localized literal context helpers", func() {
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
	})

	ginkgo.Describe("rule branches", func() {
		ginkgo.It("covers string leak and conversion early exits", func() {
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

		ginkgo.It("covers string assignment, key-value, and return diagnostics", func() {
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

		ginkgo.It("covers string value specs and direct assignment helper edges", func() {
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

		ginkgo.It("covers terminal string call boundaries", func() {
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

		ginkgo.It("covers direct cast and unchecked constructor branches", func() {
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

		ginkgo.It("covers message field, passthrough, and response status helpers", func() {
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
			_, ok := warningFieldValueMissingMessageIDNamed(&analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}, outside)
			Expect(ok).To(BeFalse())

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

		ginkgo.It("covers signature and validator helper branches", func() {
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
			paramName, typeName, ok := semanticTypeForUnnamedParam("Other", "Other", 1)
			Expect(ok).To(BeFalse())
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

			_, _, ok = semanticTypeForUnnamedParam("Other", "Other", 0)
			Expect(ok).To(BeFalse())

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
