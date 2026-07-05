package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("semantichygiene rule branches", ginkgo.Label("unit"), func() {
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
		Expect(decisionFor(callHasPrimitiveArg(h.ctx, primitiveCall))).To(Equal(ruleBranchRejected))

		generated := newRuleHarness("/repo/vendor/example/page.go", "example.com/p", `package p
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func use(raw string) { _ = NewPageIDUnchecked(raw) }
`)
		Expect(decisionFor(isAllowedUncheckedConstructorCall(generated.ctx, generated.findCall("NewPageIDUnchecked"), "PageID"))).To(Equal(ruleBranchAccepted))

		fixture := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func NewFixturePageID(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
		Expect(decisionFor(isAllowedUncheckedConstructorCall(fixture.ctx, fixture.findCall("NewPageIDUnchecked"), "PageID"))).To(Equal(ruleBranchAccepted))

		owner := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func NewPageIDUnchecked(raw any) PageID { return "" }
func MakePageID(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
		Expect(decisionFor(isAllowedUncheckedConstructorOwnerContext(owner.ctx, owner.findCall("NewPageIDUnchecked"), "PageID"))).To(Equal(ruleBranchAccepted))

		semanticConstructor := newRuleHarness("/repo/internal/core/tree/semantic_types.go", "github.com/perber/wiki/internal/core/tree", `package tree
type PageID string
func NewPageIDUnchecked(raw string) PageID { return NewPageIDUnchecked(raw) }
`)
		Expect(decisionFor(isAllowedUncheckedConstructorCall(semanticConstructor.ctx, semanticConstructor.findCall("NewPageIDUnchecked"), "PageID"))).To(Equal(ruleBranchAccepted))
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
		Expect(decisionFor(isMessageFieldValueFreeFormPassthrough(h.ctx, h.findKeyValue("Message")))).To(Equal(ruleBranchAccepted))
		checkResponseStatusForward(h.ctx, h.findKeyValue("lastError"))
		checkMessagePassthroughCall(h.ctx, h.findCall("NewLocalizedError"))
		Expect(h.diagnosticMessages()).To(ContainElements(
			ContainSubstring("passes through free-form text despite MessageID"),
			ContainSubstring("forwards message-bearing status text"),
			ContainSubstring("localized error constructor NewLocalizedError"),
		))

		Expect(decisionFor(compositeLiteralHasKey(nil, "messageID"))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(callHasFreeFormMessageArg(h.ctx, h.findCall("NewLocalizedError")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(exprSuggestsMessageStatusForward(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "lastError"}}}))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(exprSuggestsMessageStatusForward(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}}}))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(exprSuggestsMessageStatusForward(&ast.BasicLit{Kind: token.STRING, Value: `"plain"`}))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(compositeLiteralHasKey(&ast.CompositeLit{Elts: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"plain"`}}}, "messageID"))).To(Equal(ruleBranchRejected))

		outside := &ast.KeyValueExpr{Key: &ast.Ident{Name: "Message"}, Value: &ast.BasicLit{Kind: token.STRING, Value: `"plain"`}}
		checkMessageFieldValue(&analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}, outside)
		checkResponseStatusForward(h.ctx, &ast.KeyValueExpr{Key: &ast.BasicLit{Kind: token.STRING, Value: `"lastError"`}, Value: &ast.Ident{Name: "other"}})
		_, warningHasMessageID := warningFieldValueMissingMessageIDNamed(&analysisContext{parents: map[ast.Node]ast.Node{}, pass: h.ctx.pass}, outside)
		Expect(decisionFor(warningHasMessageID)).To(Equal(ruleBranchRejected))

		Expect(decisionFor(exprIsFreeFormMessageParam(h.ctx, &ast.BasicLit{Kind: token.STRING, Value: `"message"`}))).To(Equal(ruleBranchRejected))
		defsMessage := &ast.Ident{Name: "message"}
		defsVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
		defsCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
			Uses: map[*ast.Ident]types.Object{},
			Defs: map[*ast.Ident]types.Object{defsMessage: defsVar},
		}}, parents: map[ast.Node]ast.Node{}}
		Expect(decisionFor(exprIsFreeFormMessageParam(defsCtx, defsMessage))).To(Equal(ruleBranchRejected))
		nonStringMessage := &ast.Ident{Name: "message"}
		nonStringVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.Int])
		nonStringCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
			Uses: map[*ast.Ident]types.Object{nonStringMessage: nonStringVar},
		}}, parents: map[ast.Node]ast.Node{}}
		Expect(decisionFor(exprIsFreeFormMessageParam(nonStringCtx, nonStringMessage))).To(Equal(ruleBranchRejected))
		freeMessage := &ast.Ident{Name: "message"}
		freeVar := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
		freeCtx := &analysisContext{pass: &analysis.Pass{TypesInfo: &types.Info{
			Uses: map[*ast.Ident]types.Object{freeMessage: freeVar},
		}}, parents: map[ast.Node]ast.Node{}}
		Expect(decisionFor(exprIsFreeFormMessageParam(freeCtx, freeMessage))).To(Equal(ruleBranchRejected))

		otherMessage := &ast.Ident{Name: "message"}
		paramName := &ast.Ident{Name: "other"}
		param := types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "message", types.Typ[types.String])
		freeCtx.pass.TypesInfo.Defs = map[*ast.Ident]types.Object{paramName: types.NewVar(token.NoPos, types.NewPackage("example.com/p", "p"), "other", types.Typ[types.String])}
		freeCtx.pass.TypesInfo.Uses = map[*ast.Ident]types.Object{otherMessage: param}
		freeCtx.parents[otherMessage] = &ast.FuncDecl{Type: &ast.FuncType{Params: &ast.FieldList{List: []*ast.Field{{Names: []*ast.Ident{paramName}}}}}}
		Expect(decisionFor(exprIsFreeFormMessageParam(freeCtx, otherMessage))).To(Equal(ruleBranchRejected))
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
		Expect(unnamedParamClassification("Other", "Other", 1)).To(Equal(namedClassification{}))
		Expect(decisionFor(isRawStringCarrier(nil))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isRawStringCarrier(types.NewSlice(types.Typ[types.String])))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isRawStringCarrier(types.NewArray(types.Typ[types.String], 2)))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isRawStringCarrier(types.NewMap(types.Typ[types.String], types.Typ[types.Int])))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isRawStringCarrier(types.NewMap(types.Typ[types.Int], types.Typ[types.String])))).To(Equal(ruleBranchRejected))

		checkSignatureParams(h.ctx, "NoParams", "NoParams", nil)
		nilNameField := &ast.Field{Names: []*ast.Ident{nil}, Type: ast.NewIdent("string")}
		h.ctx.pass.TypesInfo.Types[nilNameField.Type] = types.TypeAndValue{Type: types.Typ[types.String]}
		checkStringSignatureField(h.ctx, nilNameField, "GetPage", "GetPage", 0)
		primitiveNilNameField := &ast.Field{Names: []*ast.Ident{nil}, Type: ast.NewIdent("int")}
		h.ctx.pass.TypesInfo.Types[primitiveNilNameField.Type] = types.TypeAndValue{Type: types.Typ[types.Int]}
		checkPrimitiveSignatureField(h.ctx, primitiveNilNameField, "Search", "SearchPage")

		Expect(unnamedParamClassification("Other", "Other", 0)).To(Equal(namedClassification{}))

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
