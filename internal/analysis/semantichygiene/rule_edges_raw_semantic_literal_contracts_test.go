package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene raw semantic literal contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies raw semantic literals across test expression shapes", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string
type Workspace struct {
	ID   WorkspaceID
	Name string
}
func acceptWorkspaceID(id WorkspaceID) {}
func acceptWorkspaceIDs(ids ...WorkspaceID) {}
func Describe(description string, body any) any { return nil }

func TestSemanticLiterals() {
	acceptWorkspaceID("call-param")
	acceptWorkspaceIDs("variadic-param")
	_ = WorkspaceID("converted")
	Describe("BDD description", nil)
	var explicit WorkspaceID = "value-spec-explicit"
	var inferred WorkspaceID = "value-spec-inferred"
	_ = []WorkspaceID{"slice-value"}
	_ = [1]WorkspaceID{"array-value"}
	_ = map[string]WorkspaceID{"key": "map-value"}
	var assigned WorkspaceID
	assigned = "assigned-value"
	_ = Workspace{ID: "field-value", Name: "display name"}
	_, _, _, _, _ = explicit, inferred, assigned, WorkspaceID("ok"), WorkspaceID("also-ok")
}
`)
		for _, value := range []string{
			"call-param",
			"variadic-param",
			"converted",
			"BDD description",
			"value-spec-explicit",
			"value-spec-inferred",
			"slice-value",
			"array-value",
			"map-value",
			"assigned-value",
			"field-value",
			"display name",
		} {
			checkTestRawSemanticLiteral(h.ctx, h.findLiteral(value))
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:semantic.test-raw-literal: raw string literal passed as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal passed as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned as WorkspaceID in test code; use a semantic fixture/helper value",
			"semh:semantic.test-raw-literal: raw string literal assigned to WorkspaceID field ID in test code; use a semantic fixture/helper value",
		))
	})

	ginkgo.It("reports fixture semantic constructors only when they wrap runtime strings", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string

const fixtureWorkspaceID = "workspace-static"

func runtimeWorkspaceID() string { return "workspace-runtime" }
func newFixtureWorkspaceID(raw string) WorkspaceID { return WorkspaceID(raw) }
func makeWorkspaceID(raw string) WorkspaceID { return WorkspaceID(raw) }
func newFixturePlain(raw string) string { return raw }

func TestFixtureConstructors() {
	_ = newFixtureWorkspaceID("workspace-static")
	_ = newFixtureWorkspaceID(fixtureWorkspaceID)
	_ = newFixtureWorkspaceID(runtimeWorkspaceID())
	_ = makeWorkspaceID(runtimeWorkspaceID())
	_ = newFixturePlain(runtimeWorkspaceID())
}
`)
		for _, call := range h.findCalls("newFixtureWorkspaceID") {
			checkFixtureSemanticConstructorCall(h.ctx, call)
		}
		checkFixtureSemanticConstructorCall(h.ctx, h.findCall("makeWorkspaceID"))
		checkFixtureSemanticConstructorCall(h.ctx, h.findCall("newFixturePlain"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:semantic.fixture-runtime-constructor: fixture semantic constructor newFixtureWorkspaceID receives runtime string; fixture constructors should only wrap static test values",
		))
	})

	ginkgo.It("keeps raw semantic literal helper branches narrow", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string
type Workspace struct {
	ID   WorkspaceID
	Name string
}

func acceptPlain(value string) {}
func acceptWorkspaceID(id WorkspaceID) {}
func acceptWorkspaceIDs(label string, ids ...WorkspaceID) {}
func Describe(description string, body any) any { return nil }

const fixtureConst = "fixture-const"

func TestSemanticLiteralEdges() {
	acceptPlain("plain-param")
	acceptWorkspaceID("semantic-param")
	acceptWorkspaceIDs("label", "semantic-variadic")
	Describe("BDD description", nil)
	var explicit WorkspaceID = "value-spec-explicit"
	var inferred WorkspaceID = "value-spec-inferred"
	assigned := WorkspaceID("seed")
	assigned = "assigned-value"
	plain := "plain-assignment"
	_ = []string{"plain-slice"}
	_ = []WorkspaceID{"semantic-slice"}
	_ = [1]WorkspaceID{"semantic-array"}
	_ = map[string]string{"key": "plain-map"}
	_ = map[string]WorkspaceID{"key": "semantic-map"}
	_ = map[string]WorkspaceID{"key" + "suffix": "semantic-map-dynamic-key"}
	_ = Workspace{ID: "field-value", Name: "display name"}
	_, _, _ = explicit, inferred, assigned
	_ = plain
	_ = fixtureConst
}
`)
		plainParam := h.findLiteral("plain-param")
		fixtureConst := lastSemanticIdentifierNamed(h.file, "fixtureConst")
		semanticParam := h.findLiteral("semantic-param")
		semanticVariadic := h.findLiteral("semantic-variadic")
		bddDescription := h.findLiteral("BDD description")
		explicitValueSpec := h.findLiteral("value-spec-explicit")
		plainAssignment := h.findLiteral("plain-assignment")
		plainSlice := h.findLiteral("plain-slice")
		semanticSlice := h.findLiteral("semantic-slice")
		semanticArray := h.findLiteral("semantic-array")
		plainMap := h.findLiteral("plain-map")
		semanticMap := h.findLiteral("semantic-map")
		dynamicKeyMap := h.findLiteral("semantic-map-dynamic-key")
		fieldValue := h.findLiteral("field-value")

		Expect(observePolicyHelperDecisions(
			semanticLiteralCallParamSucceeded(h.ctx, plainParam),
			semanticLiteralCallParamSucceeded(h.ctx, semanticParam),
			semanticLiteralCallParamSucceeded(h.ctx, semanticVariadic),
			semanticLiteralCallParamSucceeded(h.ctx, bddDescription),
			semanticLiteralAssignedValueSucceeded(h.ctx, explicitValueSpec),
			semanticLiteralAssignedValueSucceeded(h.ctx, plainAssignment),
			semanticLiteralAssignedValueSucceeded(h.ctx, plainSlice),
			semanticLiteralAssignedValueSucceeded(h.ctx, semanticSlice),
			semanticLiteralAssignedValueSucceeded(h.ctx, semanticArray),
			semanticLiteralAssignedValueSucceeded(h.ctx, plainMap),
			semanticLiteralAssignedValueSucceeded(h.ctx, semanticMap),
			semanticLiteralAssignedValueSucceeded(h.ctx, dynamicKeyMap),
			semanticLiteralCompositeFieldSucceeded(h.ctx, fieldValue),
			semanticLiteralCompositeFieldSucceeded(h.ctx, dynamicKeyMap),
			isStaticFixtureStringArg(h.ctx, plainParam),
			isStaticFixtureStringArg(h.ctx, fixtureConst),
		)).To(Equal([]policyHelperDecision{
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperAccepted,
		}))
	})

	ginkgo.It("rejects malformed literal and type lookup edges before reporting diagnostics", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type WorkspaceID string

func TestMalformedLiteralEdges() {
	var ids []WorkspaceID
	_ = ids
}
`)
		malformedString := &ast.BasicLit{Kind: token.STRING, Value: `"unterminated`}
		integerLiteral := &ast.BasicLit{Kind: token.INT, Value: "1"}
		orphanLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"orphan"`}
		manualValueName := ast.NewIdent("manualValue")
		manualValueLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"manual-value"`}
		manualValueSpec := &ast.ValueSpec{Names: []*ast.Ident{manualValueName}, Values: []ast.Expr{manualValueLiteral}}
		namelessValueLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"nameless-value"`}
		namelessValueSpec := &ast.ValueSpec{Values: []ast.Expr{namelessValueLiteral}}
		nilObjectName := ast.NewIdent("nilObject")
		nilObjectLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"nil-object"`}
		nilObjectSpec := &ast.ValueSpec{Names: []*ast.Ident{nilObjectName}, Values: []ast.Expr{nilObjectLiteral}}
		manualAssignName := ast.NewIdent("manualAssign")
		manualAssignLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"manual-assign"`}
		manualAssign := &ast.AssignStmt{Lhs: []ast.Expr{manualAssignName}, Rhs: []ast.Expr{manualAssignLiteral}}
		namelessAssignLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"nameless-assign"`}
		namelessAssign := &ast.AssignStmt{Rhs: []ast.Expr{namelessAssignLiteral}}
		h.ctx.parents[manualValueLiteral] = manualValueSpec
		h.ctx.parents[namelessValueLiteral] = namelessValueSpec
		h.ctx.parents[nilObjectLiteral] = nilObjectSpec
		h.ctx.parents[manualAssignLiteral] = manualAssign
		h.ctx.parents[namelessAssignLiteral] = namelessAssign
		h.ctx.pass.TypesInfo.Defs[manualValueName] = types.NewVar(token.NoPos, h.ctx.pass.Pkg, "manualValue", namedStringType("WorkspaceID"))
		h.ctx.pass.TypesInfo.Types[manualAssignName] = types.TypeAndValue{Type: namedStringType("WorkspaceID")}
		emptySignature := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		plainParamSignature := types.NewSignatureType(nil, nil, nil, types.NewTuple(
			types.NewVar(token.NoPos, nil, "value", types.Typ[types.String]),
		), nil, false)

		checkStableLiteral(h.ctx, malformedString)
		checkStableLiteral(h.ctx, integerLiteral)
		checkLocalizedProseLiteral(h.ctx, malformedString)
		checkLocalizedProseLiteral(h.ctx, integerLiteral)
		checkTestRawSemanticLiteral(h.ctx, malformedString)
		checkTestRawSemanticLiteral(h.ctx, integerLiteral)

		Expect(observePolicyHelperDecisions(
			semanticSignatureParamSucceeded(emptySignature, 0),
			semanticSignatureParamSucceeded(plainParamSignature, -1),
			semanticSignatureParamSucceeded(plainParamSignature, 2),
			semanticLiteralAssignedValueSucceeded(h.ctx, orphanLiteral),
			semanticLiteralAssignedValueSucceeded(h.ctx, manualValueLiteral),
			semanticLiteralAssignedValueSucceeded(h.ctx, namelessValueLiteral),
			semanticLiteralAssignedValueSucceeded(h.ctx, nilObjectLiteral),
			semanticLiteralAssignedValueSucceeded(h.ctx, manualAssignLiteral),
			semanticLiteralAssignedValueSucceeded(h.ctx, namelessAssignLiteral),
			compositeLiteralContainsDirectValue(&ast.CompositeLit{}, orphanLiteral),
			semanticCompositeLiteralValueSucceeded(h.ctx, &ast.CompositeLit{}),
			semanticCompositeLiteralMapValueSucceeded(h.ctx, &ast.CompositeLit{}),
			semanticLiteralMapValueTypeSucceeded(h.ctx, &ast.KeyValueExpr{Value: orphanLiteral}, orphanLiteral),
		)).To(Equal([]policyHelperDecision{
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
		}))
		Expect(h.diagnosticMessages()).To(BeEmpty())
	})
})

func semanticLiteralCallParamSucceeded(ctx *analysisContext, lit *ast.BasicLit) bool {
	_, ok := testLiteralSemanticCallParam(ctx, lit)
	return ok
}

func semanticLiteralAssignedValueSucceeded(ctx *analysisContext, lit *ast.BasicLit) bool {
	_, ok := testLiteralSemanticAssignedValue(ctx, lit)
	return ok
}

func semanticLiteralCompositeFieldSucceeded(ctx *analysisContext, lit *ast.BasicLit) bool {
	_, _, ok := testLiteralSemanticCompositeField(ctx, lit)
	return ok
}

func semanticSignatureParamSucceeded(sig *types.Signature, index int) bool {
	_, ok := signatureParamTypeAt(sig, index)
	return ok
}

func semanticCompositeLiteralValueSucceeded(ctx *analysisContext, composite *ast.CompositeLit) bool {
	_, ok := semanticCompositeLiteralValueType(ctx, composite)
	return ok
}

func semanticCompositeLiteralMapValueSucceeded(ctx *analysisContext, composite *ast.CompositeLit) bool {
	_, ok := semanticCompositeLiteralMapValueType(ctx, composite)
	return ok
}

func semanticLiteralMapValueTypeSucceeded(ctx *analysisContext, kv *ast.KeyValueExpr, lit *ast.BasicLit) bool {
	_, ok := testLiteralSemanticMapValueType(ctx, kv, lit)
	return ok
}

func lastSemanticIdentifierNamed(file *ast.File, name string) *ast.Ident {
	var found *ast.Ident
	ast.Inspect(file, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && ident.Name == name {
			found = ident
		}
		return true
	})
	Expect(found).NotTo(BeNil())
	return found
}
