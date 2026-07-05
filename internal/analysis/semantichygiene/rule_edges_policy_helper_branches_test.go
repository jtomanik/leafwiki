package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("semantichygiene policy helper branches", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies semantic and primitive type fallback cases", func() {
		Expect(semanticTypeClassification(nil)).To(Equal(namedClassification{}))
		Expect(semanticTypeClassification(types.Typ[types.String])).To(Equal(namedClassification{}))
		Expect(semanticTypeClassification(namedStringType("PlainID"))).To(Equal(namedClassification{}))
		Expect(semanticTypeClassification(types.NewPointer(namedStringType("WorkspaceID")))).To(Equal(namedClassification{
			Name:     "WorkspaceID",
			Decision: ruleBranchAccepted,
		}))

		Expect(decisionFor(isString(nil))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isString(types.Typ[types.UntypedString]))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isString(types.Typ[types.Int]))).To(Equal(ruleBranchRejected))

		Expect(primitiveCarrierClassification(nil)).To(Equal(namedClassification{}))
		Expect(primitiveCarrierClassification(types.NewStruct(nil, nil))).To(Equal(namedClassification{}))
		Expect(primitiveCarrierClassification(types.Typ[types.Uint32])).To(Equal(namedClassification{
			Name:     "uint32",
			Decision: ruleBranchAccepted,
		}))
		Expect(primitiveCarrierClassification(types.Typ[types.Float64])).To(Equal(namedClassification{}))

		Expect(decisionFor(semanticConstructorAllowsSource("", "ErrorCode"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(semanticConstructorAllowsSource("MessageID", "ErrorCode"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(semanticConstructorAllowsSource("MessageID", "UserID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(semanticConstructorAllowsSource("ToolDescriptionID", "ToolID"))).To(Equal(ruleBranchAccepted))
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
		Expect(decisionFor(isConstOrTypeDefinition(ctx, target))).To(Equal(ruleBranchRejected))

		fn := &ast.FuncDecl{Name: &ast.Ident{Name: "Run"}}
		ctx.parents[target] = fn
		Expect(enclosingFuncName(ctx, target)).To(Equal("Run"))
		Expect(enclosingFunc(ctx, target)).To(Equal(fn))
		Expect(decisionFor(isConstOrTypeDefinition(ctx, target))).To(Equal(ruleBranchRejected))
	})

	ginkgo.It("classifies AST struct and composite helper fallback cases", func() {
		strct := &ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
			{Names: []*ast.Ident{nil}},
			{Names: []*ast.Ident{{Name: "StatusCode"}}},
		}}}
		Expect(decisionFor(astStructHasField(strct, "messageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(astStructHasField(&ast.StructType{Fields: &ast.FieldList{List: []*ast.Field{
			{Names: []*ast.Ident{{Name: "MessageID"}}},
		}}}, "messageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(astStructHasContractSignal(strct))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(astStructIsMessageBearing("Plain", strct))).To(Equal(ruleBranchAccepted))

		target := &ast.Ident{Name: "target"}
		fn := &ast.FuncDecl{Name: &ast.Ident{Name: "Stop"}}
		ctx := &analysisContext{parents: map[ast.Node]ast.Node{target: fn}}
		named, typedStruct, lit, hasCompositeContext := enclosingNamedCompositeStructLiteral(ctx, target)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchRejected))
		Expect(named).To(BeNil())
		Expect(typedStruct).To(BeNil())
		Expect(lit).To(BeNil())

		ctx.parents = map[ast.Node]ast.Node{}
		_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, target)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchRejected))

		litNode := &ast.CompositeLit{}
		ctx.pass = &analysis.Pass{TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
			litNode: {Type: types.NewStruct(nil, nil)},
		}}}
		_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, litNode)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchRejected))

		namedBasic := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "NamedBasic", nil), types.Typ[types.String], nil)
		ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: namedBasic}
		_, _, _, hasCompositeContext = enclosingNamedCompositeStructLiteral(ctx, litNode)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchRejected))

		namedStruct := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "Payload", nil), types.NewStruct(nil, nil), nil)
		ctx.pass.TypesInfo.Types[litNode] = types.TypeAndValue{Type: types.NewPointer(namedStruct)}
		gotNamed, gotStruct, gotLit, hasCompositeContext := enclosingNamedCompositeStructLiteral(ctx, litNode)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchAccepted))
		Expect(gotNamed.Obj().Name()).To(Equal("Payload"))
		Expect(gotStruct.NumFields()).To(BeZero())
		Expect(gotLit).To(Equal(litNode))
		wrappedNamed, wrappedStruct, hasCompositeContext := enclosingNamedCompositeStruct(ctx, litNode)
		Expect(decisionFor(hasCompositeContext)).To(Equal(ruleBranchAccepted))
		Expect(wrappedNamed).To(Equal(gotNamed))
		Expect(wrappedStruct).To(Equal(gotStruct))
	})

	ginkgo.DescribeTable("classifies remaining edge adapter filename cases",
		func(filename string) {
			Expect(decisionFor(isEdgeAdapterFile(filename))).To(Equal(ruleBranchAccepted))
		},
		ginkgo.Entry("tree migration adapter", "/repo/internal/core/tree/migration_adapter.go"),
		ginkgo.Entry("workspacesync watcher adapter", "/repo/internal/workspacesync/watcher_adapter.go"),
	)

	ginkgo.DescribeTable("classifies owner adapter files across packages",
		func(packagePath string, filename string, want ruleBranchDecision) {
			h := newRuleHarness(filename, packagePath, `package p`)
			Expect(decisionFor(isSemanticOwnerAdapterFile(h.ctx, h.file.Package))).To(Equal(want))
		},
		ginkgo.Entry("auth semantic owner", "github.com/perber/wiki/internal/core/auth", "/repo/internal/core/auth/semantic_types.go", ruleBranchAccepted),
		ginkgo.Entry("revision semantic owner", "github.com/perber/wiki/internal/core/revision", "/repo/internal/core/revision/semantic_types.go", ruleBranchAccepted),
		ginkgo.Entry("markdown validation semantic owner", "github.com/perber/wiki/internal/core/markdownvalidation", "/repo/internal/core/markdownvalidation/issue_codes.go", ruleBranchAccepted),
		ginkgo.Entry("workspacesync semantic owner", "github.com/perber/wiki/internal/workspacesync", "/repo/internal/workspacesync/semantic_types.go", ruleBranchAccepted),
		ginkgo.Entry("workspaceid semantic owner", "github.com/perber/wiki/internal/workspaceid", "/repo/internal/workspaceid/validate.go", ruleBranchAccepted),
		ginkgo.Entry("ordinary package", "github.com/perber/wiki/internal/wiki/pages", "/repo/internal/wiki/pages/page.go", ruleBranchRejected),
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
		Expect(decisionFor(isAllowedDirectCastContext(generated.ctx, generated.findCall("PageID"), "PageID"))).To(Equal(ruleBranchAccepted))

		testLiteral := newRuleHarness("/repo/internal/p/page_test.go", "example.com/p", `package p
type PageID string
func literal() PageID { return PageID("page-1") }
		`)
		Expect(decisionFor(isAllowedDirectCastContext(testLiteral.ctx, testLiteral.findCall("PageID"), "PageID"))).To(Equal(ruleBranchRejected))

		fixture := newRuleHarness("/repo/internal/p/page_test.go", "example.com/p", source)
		Expect(decisionFor(isAllowedDirectCastContext(fixture.ctx, fixture.findCall("PageID"), "PageID"))).To(Equal(ruleBranchAccepted))

		edge := newRuleHarness("/repo/internal/wiki/import_adapter.go", "example.com/p", source)
		Expect(decisionFor(isAllowedDirectCastContext(edge.ctx, edge.findCalls("PageID")[1], "PageID"))).To(Equal(ruleBranchAccepted))

		normal := newRuleHarness("/repo/internal/p/page.go", "example.com/p", source)
		normalCasts := normal.findCalls("PageID")
		Expect(decisionFor(isAllowedDirectCastContext(normal.ctx, normalCasts[0], "PageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isConstOrTypeDefinition(normal.ctx, normalCasts[0]))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedDirectCastContext(normal.ctx, normalCasts[len(normalCasts)-1], "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isConstOrTypeDefinition(normal.ctx, normalCasts[len(normalCasts)-1]))).To(Equal(ruleBranchAccepted))
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

		Expect(decisionFor(functionReturnsSemanticType(h.ctx, noResult, "PageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(functionReturnsSemanticType(h.ctx, constructor, "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(functionHasSemanticReceiver(h.ctx, constructor, "PageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(functionHasSemanticReceiver(h.ctx, value, "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(functionHasSemanticParameter(h.ctx, noResult, "PageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(functionHasSemanticParameter(h.ctx, scan, "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedSemanticOwnerAdapterFunc(h.ctx, h.findCall("string"), "PageID"))).To(Equal(ruleBranchAccepted))

		Expect(decisionFor(typeContainsSemanticType(nil, "PageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(typeContainsSemanticType(types.NewSlice(namedStringType("PageID")), "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(typeContainsSemanticType(types.NewArray(namedStringType("PageID"), 2), "PageID"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(typeContainsSemanticType(types.Typ[types.String], "PageID"))).To(Equal(ruleBranchRejected))

		emptyParams := &ast.FuncDecl{Name: &ast.Ident{Name: "NoParams"}, Type: &ast.FuncType{}}
		Expect(decisionFor(functionHasSemanticParameter(h.ctx, emptyParams, "PageID"))).To(Equal(ruleBranchRejected))

		plain := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func Plain(raw string) string { return raw }
`)
		Expect(decisionFor(isAllowedSemanticOwnerAdapterFunc(plain.ctx, plain.findFunc("Plain"), "PageID"))).To(Equal(ruleBranchRejected))
	})

	ginkgo.It("classifies wire persistence and JSON composite contexts", func() {
		wire := newRuleHarness("/repo/internal/service/page.go", "example.com/p", "package p\n"+
			"type PageResponse struct {\n"+
			"  PageID string `json:\"pageId\"`\n"+
			"  Plain string\n"+
			"}\n"+
			"func build(pageID string) PageResponse { return PageResponse{PageID: pageID, Plain: pageID} }\n")
		Expect(decisionFor(inJSONCompositeLiteral(wire.ctx, wire.findKeyValue("PageID").Value))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(inJSONCompositeLiteral(wire.ctx, wire.findKeyValue("Plain").Value))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(compositeFieldHasWireTag(wire.ctx, wire.firstCompositeLiteral(), "Missing"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedWireComposite(wire.ctx, wire.firstCompositeLiteral(), "PageResponse", wire.file.Package))).To(Equal(ruleBranchAccepted))

		store := newRuleHarness("/repo/internal/wiki/page_store.go", "example.com/p", `package p
type pageRecord struct { PageID string }
type pageModel struct { PageID string }
func build(pageID string) pageRecord { return pageRecord{PageID: pageID} }
`)
		Expect(decisionFor(isPersistenceRowStruct(store.ctx, store.findTypeSpec("pageRecord")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isPersistenceRowStruct(store.ctx, store.findTypeSpec("pageModel")))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedPersistenceRowKeyValue(store.ctx, store.findKeyValue("PageID").Value))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isPersistenceRowComposite(store.ctx, store.firstCompositeLiteral()))).To(Equal(ruleBranchAccepted))

		nonStore := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type pageRecord struct { PageID string }
func build(pageID string) pageRecord { return pageRecord{PageID: pageID} }
`)
		Expect(decisionFor(isPersistenceRowComposite(nonStore.ctx, nonStore.firstCompositeLiteral()))).To(Equal(ruleBranchRejected))

		pointerStore := newRuleHarness("/repo/internal/wiki/page_store.go", "example.com/p", `package p
type pageRecord struct { PageID string }
func build(pageID string) *pageRecord { return &pageRecord{PageID: pageID} }
`)
		Expect(decisionFor(isPersistenceRowComposite(pointerStore.ctx, pointerStore.firstCompositeLiteral()))).To(Equal(ruleBranchAccepted))

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
		Expect(decisionFor(isPersistenceRowComposite(pointerCtx, pointerComposite))).To(Equal(ruleBranchAccepted))

		orphan := &ast.Ident{Name: "orphan"}
		funcBarrier := &ast.FuncDecl{Name: &ast.Ident{Name: "stop"}}
		manualCtx := &analysisContext{parents: map[ast.Node]ast.Node{orphan: funcBarrier}}
		Expect(decisionFor(inJSONCompositeLiteral(manualCtx, orphan))).To(Equal(ruleBranchRejected))
		manualCtx.parents = map[ast.Node]ast.Node{}
		Expect(decisionFor(inJSONCompositeLiteral(manualCtx, orphan))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedPersistenceRowKeyValue(manualCtx, orphan))).To(Equal(ruleBranchRejected))
		manualCtx.parents[orphan] = funcBarrier
		Expect(decisionFor(isAllowedPersistenceRowKeyValue(manualCtx, orphan))).To(Equal(ruleBranchRejected))

		pageIDField := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "PageID", types.Typ[types.String])
		taggedStruct := types.NewStruct([]*types.Var{pageIDField}, []string{`json:"pageId"`})
		pageResponse := types.NewNamed(types.NewTypeName(token.NoPos, types.NewPackage("example.com/p", "p"), "PageResponse", nil), taggedStruct, nil)
		composite := &ast.CompositeLit{}
		manualCtx.pass = &analysis.Pass{Fset: token.NewFileSet(), TypesInfo: &types.Info{Types: map[ast.Expr]types.TypeAndValue{
			composite: {Type: types.NewPointer(pageResponse)},
		}}}
		Expect(decisionFor(compositeFieldHasWireTag(manualCtx, composite, "PageID"))).To(Equal(ruleBranchAccepted))

		manualCtx.pass.TypesInfo.Types[composite] = types.TypeAndValue{Type: types.NewStruct(nil, nil)}
		Expect(decisionFor(compositeFieldHasWireTag(manualCtx, composite, "PageID"))).To(Equal(ruleBranchRejected))
		manualCtx.pass.TypesInfo.Types[composite] = types.TypeAndValue{Type: namedStringType("PageResponse")}
		Expect(decisionFor(compositeFieldHasWireTag(manualCtx, composite, "PageID"))).To(Equal(ruleBranchRejected))
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

		Expect(decisionFor(isStableContractLiteral(h.ctx, stableCall, "page_not_found"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(stableLiteralContextSuggestsContract(h.ctx, stableReturn))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(stableLiteralContextSuggestsContract(h.ctx, plain))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isStableMessageLikeLiteral("validation.page.title_required"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isStableMessageLikeLiteral("wiki_get_page"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isStableMessageLikeLiteral("page_not_found"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isStableMessageLikeLiteral("plain prose"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isLikelyErrorCodeLiteral("page_invalid_kind"))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isLikelyErrorCodeLiteral("access_token"))).To(Equal(ruleBranchRejected))

		Expect(decisionFor(isStableLiteralAllowed(h.ctx, stableCall))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isLocalizedProseLiteralAllowed(h.ctx, plain))).To(Equal(ruleBranchRejected))
		testFile := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func plain() string { return "hello world" }
`)
		Expect(decisionFor(isStableLiteralAllowed(testFile.ctx, testFile.findLiteral("hello world")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isLocalizedProseLiteralAllowed(testFile.ctx, testFile.findLiteral("hello world")))).To(Equal(ruleBranchAccepted))

		plainCall := &ast.CallExpr{Args: []ast.Expr{
			&ast.Ident{Name: "notLiteral"},
			&ast.BasicLit{Kind: token.INT, Value: "1"},
			&ast.BasicLit{Kind: token.STRING, Value: `"validation.page.required"`},
		}}
		Expect(decisionFor(localizedProseConstructorRequiresCatalogOnly("OtherConstructor", h.ctx, plainCall))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(callHasRawStableContractArg(h.ctx, &ast.CallExpr{}))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(callContainsArg(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}}}, stableCall))).To(Equal(ruleBranchRejected))

		kv := &ast.KeyValueExpr{Key: &ast.Ident{Name: "errorCode"}}
		matches, terminal := stableLiteralContextDecision(h.ctx, kv, stableCall)
		Expect(decisionFor(matches)).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(terminal)).To(Equal(ruleBranchRejected))
		assign := &ast.AssignStmt{Lhs: []ast.Expr{&ast.Ident{Name: "errorCode"}}, Rhs: []ast.Expr{stableCall}}
		matches, terminal = stableLiteralContextDecision(h.ctx, assign, stableCall)
		Expect(decisionFor(matches)).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(terminal)).To(Equal(ruleBranchRejected))
		spec := &ast.ValueSpec{Names: []*ast.Ident{{Name: "errorCode"}}, Values: []ast.Expr{stableCall}}
		matches, terminal = stableLiteralContextDecision(h.ctx, spec, stableCall)
		Expect(decisionFor(matches)).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(terminal)).To(Equal(ruleBranchRejected))
		Expect(decisionFor(stableLiteralContextSuggestsContract(&analysisContext{parents: map[ast.Node]ast.Node{}}, stableCall))).To(Equal(ruleBranchRejected))

		Expect(decisionFor(isCLIOutputWriterExpr(&ast.Ident{Name: "stdout"}))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isResponsePayloadLiteral(&analysisContext{parents: map[ast.Node]ast.Node{plain: &ast.FuncDecl{}}}, plain))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isResponsePayloadLiteral(&analysisContext{parents: map[ast.Node]ast.Node{}}, plain))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isStrictLocalizedProseContractLiteral(&analysisContext{parents: map[ast.Node]ast.Node{}}, plain, "plain prose"))).To(Equal(ruleBranchRejected))

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
