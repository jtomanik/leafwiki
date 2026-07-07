package testhygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/tools/go/analysis"
)

var _ = ginkgo.Describe("testhygiene semantic name policy contracts", ginkgo.Label("unit"), func() {
	ginkgo.It("recognizes semantic named types and raw primitives without widening ordinary strings", func() {
		semanticPackage := types.NewPackage("github.com/perber/wiki/internal/core/tree", "tree")
		externalPackage := types.NewPackage("example.com/external", "external")
		workspaceID := types.NewNamed(types.NewTypeName(token.NoPos, semanticPackage, "WorkspaceID", nil), types.Typ[types.String], nil)
		plainType := types.NewNamed(types.NewTypeName(token.NoPos, semanticPackage, "PlainValue", nil), types.Typ[types.String], nil)
		externalType := types.NewNamed(types.NewTypeName(token.NoPos, externalPackage, "WorkspaceID", nil), types.Typ[types.String], nil)
		ctx := &analysisContext{pass: &analysis.Pass{Pkg: semanticPackage}}
		_, nilType := semanticTypeNameOf(nil)
		_, primitiveType := semanticTypeNameOf(types.Typ[types.String])
		_, semanticPointerType := semanticTypeNameOf(types.NewPointer(workspaceID))
		_, plainNamedType := semanticTypeNameOf(plainType)

		Expect(observeHelperDecisions(
			nilType,
			primitiveType,
			semanticPointerType,
			plainNamedType,
			isString(nil),
			isString(types.Typ[types.String]),
			isString(types.Typ[types.UntypedString]),
			isString(types.Typ[types.Int]),
			nameSuggestsAssetNameContext("assetName"),
			nameSuggestsAssetNameContext("pageName"),
			typeContainsLeafWikiDomainType(ctx, types.NewMap(types.Typ[types.String], types.NewSlice(types.NewPointer(workspaceID)))),
			typeContainsLeafWikiDomainType(ctx, types.NewArray(externalType, 2)),
		)).To(Equal([]helperDecision{
			helperRejected,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
		}))
	})

	ginkgo.It("infers semantic IDs and hashes from narrow helper contexts", func() {
		_, apiKeyID := semanticTypeForBareIDContext("apiKeyLookup")
		_, workspaceID := semanticTypeForBareIDContext("workspaceLookup")
		_, revisionID := semanticTypeForBareIDContext("revisionLookup")
		_, userID := semanticTypeForBareIDContext("authorLookup")
		_, toolID := semanticTypeForBareIDContext("toolLookup")
		_, pageID := semanticTypeForBareIDContext("nodeLookup")
		_, byID := semanticTypeForBareIDContext("findByID")
		_, unknownID := semanticTypeForBareIDContext("assetLookup")
		_, fieldHash := semanticTypeForFieldName("hash", "RevisionRecord")
		_, ordinaryFieldHash := semanticTypeForFieldName("hash", "AssetRecord")
		_, paramHash := semanticTypeForParamName("hash", "loadCommit")
		_, ordinaryParamHash := semanticTypeForParamName("hash", "loadAsset")
		_, pluralSlug := semanticTypeForCanonicalName("slugs")

		Expect(observeHelperDecisions(
			apiKeyID,
			workspaceID,
			revisionID,
			userID,
			toolID,
			pageID,
			byID,
			unknownID,
			fieldHash,
			ordinaryFieldHash,
			paramHash,
			ordinaryParamHash,
			pluralSlug,
			semanticName("workspace_id"),
			semanticName("plain"),
		)).To(Equal([]helperDecision{
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperRejected,
			helperAccepted,
			helperAccepted,
			helperRejected,
		}))
	})

	ginkgo.It("extracts expression and call names from supported AST shapes", func() {
		Expect([]string{
			exprName(ast.NewIdent("messageID")),
			exprName(&ast.SelectorExpr{Sel: ast.NewIdent("MessageID")}),
			exprName(&ast.StarExpr{X: ast.NewIdent("WorkspaceID")}),
			exprName(&ast.IndexExpr{X: ast.NewIdent("Rows")}),
			exprName(&ast.IndexListExpr{X: ast.NewIdent("Rows")}),
			exprName(&ast.BasicLit{Kind: token.STRING, Value: `"literal"`}),
			callName(&ast.CallExpr{Fun: ast.NewIdent("Expect")}),
			callName(&ast.CallExpr{Fun: &ast.SelectorExpr{Sel: ast.NewIdent("To")}}),
			callName(&ast.CallExpr{Fun: &ast.BasicLit{Kind: token.STRING, Value: `"literal"`}}),
		}).To(Equal([]string{
			"messageID",
			"MessageID",
			"WorkspaceID",
			"Rows",
			"Rows",
			"",
			"Expect",
			"To",
			"",
		}))
	})
})
