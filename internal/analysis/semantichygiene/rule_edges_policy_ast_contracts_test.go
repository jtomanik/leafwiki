package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type policyHelperDecision uint8

const (
	policyHelperRejected policyHelperDecision = iota
	policyHelperAccepted
)

var _ = ginkgo.Describe("semantichygiene policy AST helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies matcher factory names and protocol keys without accepting ordinary names", func() {
		Expect(observePolicyHelperDecisions(
			structuredProtocolKeyName("message_id"),
			structuredProtocolKeyName("status"),
			gomegaRenderedOutputMatcherFactoryName("HaveFatalOutput"),
			gomegaRenderedOutputMatcherFactoryName("CollectFatalOutput"),
			gomegaMatcherFactoryName("ContainPage"),
			gomegaMatcherFactoryName("BuildPage"),
			isTestContractAssertionCall(nil, &ast.CallExpr{Fun: ast.NewIdent("assertMessageID")}),
			isTestContractAssertionCall(nil, &ast.CallExpr{Fun: ast.NewIdent("requireMessageID")}),
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
		}))
		Expect(identifierWords("HTTPServerID-set")).To(Equal([]string{"http", "server", "id", "set"}))
	})

	ginkgo.It("recognizes Gomega matcher return types across identifier and selector shapes", func() {
		Expect(observePolicyHelperDecisions(
			funcReturnsGomegaMatcher(functionReturning(ast.NewIdent("GomegaMatcher"))),
			funcReturnsGomegaMatcher(functionReturning(&ast.SelectorExpr{Sel: ast.NewIdent("GomegaMatcher")})),
			funcReturnsGomegaMatcher(functionReturning(ast.NewIdent("string"))),
			funcReturnsGomegaMatcher(&ast.FuncDecl{Type: &ast.FuncType{}}),
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
		}))
	})

	ginkgo.It("recognizes named semantic types and bare hash contexts", func() {
		semanticPackage := types.NewPackage("github.com/perber/wiki/internal/core/revision", "revision")
		commitHash := types.NewNamed(types.NewTypeName(token.NoPos, semanticPackage, "CommitHash", nil), types.Typ[types.String], nil)
		otherType := types.NewNamed(types.NewTypeName(token.NoPos, semanticPackage, "Other", nil), types.Typ[types.String], nil)
		_, revisionFieldHash := semanticTypeForFieldName("hash", "RevisionRecord")
		_, assetFieldHash := semanticTypeForFieldName("hash", "AssetRecord")
		_, commitParamHash := semanticTypeForParamName("hash", "loadCommit")
		_, assetParamHash := semanticTypeForParamName("hash", "loadAsset")

		Expect(observePolicyHelperDecisions(
			isNamedTypeFromPackage(commitHash, "github.com/perber/wiki/internal/core/revision", "CommitHash"),
			isNamedTypeFromPackage(types.NewPointer(commitHash), "github.com/perber/wiki/internal/core/revision", "CommitHash"),
			isNamedTypeFromPackage(otherType, "github.com/perber/wiki/internal/core/revision", "CommitHash"),
			isNamedTypeFromPackage(nil, "github.com/perber/wiki/internal/core/revision", "CommitHash"),
			revisionFieldHash,
			assetFieldHash,
			commitParamHash,
			assetParamHash,
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
		}))
	})

	ginkgo.It("unwraps parenthesized expressions to the innermost subject", func() {
		expr := &ast.ParenExpr{X: &ast.ParenExpr{X: ast.NewIdent("messageID")}}

		Expect(exprName(unparenExpr(expr))).To(Equal("messageID"))
	})
})

func functionReturning(expr ast.Expr) *ast.FuncDecl {
	return &ast.FuncDecl{
		Type: &ast.FuncType{
			Results: &ast.FieldList{
				List: []*ast.Field{{Type: expr}},
			},
		},
	}
}

func observePolicyHelperDecisions(values ...bool) []policyHelperDecision {
	decisions := make([]policyHelperDecision, 0, len(values))
	for _, value := range values {
		if value {
			decisions = append(decisions, policyHelperAccepted)
			continue
		}
		decisions = append(decisions, policyHelperRejected)
	}
	return decisions
}
