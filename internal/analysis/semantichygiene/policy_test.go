package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("policy helpers", func() {
	type fileBoundaryCase struct {
		input             string
		testFile          bool
		generatedVendored bool
		persistence       bool
		edge              bool
	}

	ginkgo.DescribeTable("canonicalName normalizes semantic identifiers",
		func(input string, want string) {
			Expect(canonicalName(input)).To(Equal(want))
		},
		ginkgo.Entry("camel case", "WorkspaceID", "workspaceid"),
		ginkgo.Entry("underscore", "workspace_id", "workspaceid"),
		ginkgo.Entry("hyphen", "workspace-id", "workspaceid"),
		ginkgo.Entry("mixed delimiters", "Workspace-Source_Path", "workspacesourcepath"),
	)

	ginkgo.DescribeTable("semanticTypeForFieldName recognizes direct and contextual names",
		func(fieldName string, typeName string, wantType string, wantOK bool) {
			got, ok := semanticTypeForFieldName(fieldName, typeName)
			Expect(ok).To(Equal(wantOK))
			Expect(got).To(Equal(wantType))
		},
		ginkgo.Entry("direct workspace id", "WorkspaceID", "", "WorkspaceID", true),
		ginkgo.Entry("direct route path plural", "route_paths", "", "RoutePath", true),
		ginkgo.Entry("page context bare id", "ID", "PageStatus", "PageID", true),
		ginkgo.Entry("node context bare id", "ID", "TreeNode", "PageID", true),
		ginkgo.Entry("permalink context bare id", "ID", "PermalinkRecord", "PageID", true),
		ginkgo.Entry("non-semantic field", "Name", "UserProfile", "", false),
	)

	ginkgo.DescribeTable("semanticTypeForParamName recognizes parameter and function context",
		func(paramName string, funcName string, wantType string, wantOK bool) {
			got, ok := semanticTypeForParamName(paramName, funcName)
			Expect(ok).To(Equal(wantOK))
			Expect(got).To(Equal(wantType))
		},
		ginkgo.Entry("direct user id", "userID", "", "UserID", true),
		ginkgo.Entry("workspace bare id", "id", "LoadWorkspace", "WorkspaceID", true),
		ginkgo.Entry("revision bare id", "id", "RestoreRevision", "RevisionID", true),
		ginkgo.Entry("user bare id", "id", "FindUser", "UserID", true),
		ginkgo.Entry("tool bare id", "id", "RunTool", "ToolID", true),
		ginkgo.Entry("page bare id", "id", "GetPage", "PageID", true),
		ginkgo.Entry("byID suffix", "id", "FindByID", "PageID", true),
		ginkgo.Entry("non-semantic miss", "name", "FindProfile", "", false),
	)

	ginkgo.DescribeTable("semanticTypeForBareIDContext resolves stable ID contexts",
		func(context string, wantType string, wantOK bool) {
			got, ok := semanticTypeForBareIDContext(context)
			Expect(ok).To(Equal(wantOK))
			Expect(got).To(Equal(wantType))
		},
		ginkgo.Entry("api key", "LookupAPIKey", "APIKeyID", true),
		ginkgo.Entry("workspace", "WorkspaceStatus", "WorkspaceID", true),
		ginkgo.Entry("revision", "RevisionBackend", "RevisionID", true),
		ginkgo.Entry("author", "AuthorProfile", "UserID", true),
		ginkgo.Entry("tool", "ToolDescriptor", "ToolID", true),
		ginkgo.Entry("node page context", "NodeBySlug", "PageID", true),
		ginkgo.Entry("miss", "Profile", "", false),
	)

	ginkgo.DescribeTable("semanticPrimitiveNameInContext requires contextual hints",
		func(name string, context string, want bool) {
			Expect(semanticPrimitiveNameInContext(name, context)).To(Equal(want))
		},
		ginkgo.Entry("offset in search context", "offset", "SearchRequest", true),
		ginkgo.Entry("offset in page context", "offset", "PageQuery", true),
		ginkgo.Entry("offset outside hinted context", "offset", "WorkspaceRequest", false),
		ginkgo.Entry("limit in revision context", "limit", "RevisionList", true),
		ginkgo.Entry("limit in page context", "limit", "PageSearch", true),
		ginkgo.Entry("maxBytes in asset context", "maxBytes", "AssetUpload", true),
		ginkgo.Entry("maxBytes in stream context", "max_bytes", "WriteStream", true),
		ginkgo.Entry("depth in tree context", "depth", "SubtreeNavigation", true),
		ginkgo.Entry("unknown primitive", "count", "SearchRequest", false),
	)

	ginkgo.DescribeTable("semanticPrimitiveName recognizes primitive carriers directly",
		func(name string, want bool) {
			Expect(semanticPrimitiveName(name)).To(Equal(want))
		},
		ginkgo.Entry("depth", "Depth", true),
		ginkgo.Entry("limit", "Limit", true),
		ginkgo.Entry("offset", "offset", true),
		ginkgo.Entry("ordinary count", "count", false),
	)

	ginkgo.DescribeTable("semanticTypeForName returns precise names or a generic fallback",
		func(name string, want string) {
			Expect(semanticTypeForName(name)).To(Equal(want))
		},
		ginkgo.Entry("known semantic type", "workspace_id", "WorkspaceID"),
		ginkgo.Entry("unknown semantic-ish type", "external token", "a semantic type"),
	)

	ginkgo.DescribeTable("file boundary helpers classify stable package paths",
		func(row fileBoundaryCase) {
			Expect(isTestFile(row.input)).To(Equal(row.testFile))
			Expect(isGeneratedOrVendored(row.input)).To(Equal(row.generatedVendored))
			Expect(isPersistenceAdapterFile(row.input)).To(Equal(row.persistence))
			Expect(isEdgeAdapterFile(row.input)).To(Equal(row.edge))
		},
		ginkgo.Entry("test file", fileBoundaryCase{input: "/repo/internal/core/page_test.go", testFile: true}),
		ginkgo.Entry("vendor file", fileBoundaryCase{input: "/repo/vendor/example/pkg/file.go", generatedVendored: true}),
		ginkgo.Entry("node modules file", fileBoundaryCase{input: "/repo/ui/node_modules/pkg/file.go", generatedVendored: true}),
		ginkgo.Entry("store file", fileBoundaryCase{input: "/repo/internal/core/page_store.go", persistence: true}),
		ginkgo.Entry("revision fs store", fileBoundaryCase{input: "/repo/internal/core/revision/fs_store.go", persistence: true}),
		ginkgo.Entry("wiki import adapter", fileBoundaryCase{input: "/repo/internal/wiki/import_adapter.go", edge: true}),
		ginkgo.Entry("regular production file", fileBoundaryCase{input: "/repo/internal/core/page.go"}),
	)

	ginkgo.DescribeTable("allowed signature and struct-field files stay narrow",
		func(filename string, signatureAllowed bool, structAllowed bool) {
			Expect(isAllowedSignatureFile(filename)).To(Equal(signatureAllowed))
			Expect(isAllowedStructFieldFile(filename)).To(Equal(structAllowed))
		},
		ginkgo.Entry("repo test boundary", "/repo/internal/analysis/semantichygiene/testdata/repotests/repo_test.go", true, false),
		ginkgo.Entry("http dto", "/repo/internal/http/dto/page.go", true, false),
		ginkgo.Entry("mcp wire types", "/repo/internal/wiki/mcp/types.go", true, false),
		ginkgo.Entry("markdown serialization", "/repo/internal/core/markdown/metadata.go", true, false),
		ginkgo.Entry("test support", "/repo/internal/test_utils/common.go", true, true),
		ginkgo.Entry("ordinary production file", "/repo/internal/wiki/page.go", false, false),
	)

	ginkgo.DescribeTable("string boundary files stay narrower than general test support",
		func(filename string, want bool) {
			Expect(isAllowedStringBoundaryFile(filename)).To(Equal(want))
		},
		ginkgo.Entry("generated", "/repo/vendor/example/pkg/file.go", true),
		ginkgo.Entry("test matcher support", "/repo/internal/test_utils/matchers/matchers.go", true),
		ginkgo.Entry("ordinary test support", "/repo/internal/test_utils/common.go", false),
		ginkgo.Entry("ordinary production", "/repo/internal/wiki/page.go", false),
	)

	ginkgo.It("classifies typed message-bearing structs and message ID fields", func() {
		pkg := types.NewPackage("example.com/policy", "policy")
		field := func(name string) *types.Var {
			return types.NewVar(token.NoPos, pkg, name, types.Typ[types.String])
		}
		named := func(name string, fields ...*types.Var) (*types.Named, *types.Struct) {
			strct := types.NewStruct(fields, make([]string, len(fields)))
			return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), strct, nil), strct
		}

		plain, plainStruct := named("PlainPayload", field("Name"))
		validation, validationStruct := named("ValidationResult")
		coded, codedStruct := named("PlainPayload", field("StatusCode"))
		messageID, messageIDStruct := named("PlainPayload", field("MessageID"))

		Expect(namedStructIsMessageBearing(nil, plainStruct)).To(BeFalse())
		Expect(namedStructIsMessageBearing(plain, nil)).To(BeFalse())
		Expect(namedStructIsMessageBearing(plain, plainStruct)).To(BeFalse())
		Expect(namedStructIsMessageBearing(validation, validationStruct)).To(BeTrue())
		Expect(namedStructIsMessageBearing(coded, codedStruct)).To(BeTrue())

		Expect(namedStructHasMessageID(nil, messageIDStruct)).To(BeFalse())
		Expect(namedStructHasMessageID(messageID, nil)).To(BeFalse())
		Expect(namedStructHasMessageID(plain, plainStruct)).To(BeFalse())
		Expect(namedStructHasMessageID(messageID, messageIDStruct)).To(BeTrue())
	})

	ginkgo.It("recognizes literal, assignment, and value-spec stable contract contexts", func() {
		empty := &ast.BasicLit{Kind: token.STRING, Value: `""`}
		root := &ast.BasicLit{Kind: token.STRING, Value: `"root"`}
		other := &ast.BasicLit{Kind: token.STRING, Value: `"other"`}
		malformed := &ast.BasicLit{Kind: token.STRING, Value: `"`}
		intLiteral := &ast.BasicLit{Kind: token.INT, Value: `0`}

		Expect(isEmptyOrRootString(empty)).To(BeTrue())
		Expect(isEmptyOrRootString(root)).To(BeTrue())
		Expect(isEmptyOrRootString(other)).To(BeFalse())
		Expect(isEmptyOrRootString(malformed)).To(BeFalse())
		Expect(isEmptyOrRootString(intLiteral)).To(BeFalse())

		contractLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"validation.error"`}
		plainLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"plain"`}
		assign := &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.Ident{Name: "errorCode"}},
			Rhs: []ast.Expr{contractLiteral},
		}
		Expect(assignStmtValueNameSuggestsStableContract(assign, contractLiteral)).To(BeTrue())
		Expect(assignStmtValueNameSuggestsStableContract(assign, plainLiteral)).To(BeFalse())
		Expect(assignStmtValueNameSuggestsStableContract(&ast.AssignStmt{Rhs: []ast.Expr{contractLiteral}}, contractLiteral)).To(BeFalse())

		spec := &ast.ValueSpec{
			Names:  []*ast.Ident{{Name: "messageID"}},
			Values: []ast.Expr{contractLiteral},
		}
		Expect(valueSpecNameSuggestsStableContract(spec, contractLiteral)).To(BeTrue())
		Expect(valueSpecNameSuggestsStableContract(spec, plainLiteral)).To(BeFalse())
		Expect(valueSpecNameSuggestsStableContract(&ast.ValueSpec{Values: []ast.Expr{contractLiteral}}, contractLiteral)).To(BeFalse())
	})

	ginkgo.It("handles AST name, call containment, key name, and map helper edge cases", func() {
		target := &ast.Ident{Name: "target"}
		call := &ast.CallExpr{
			Fun:  &ast.BasicLit{Kind: token.STRING, Value: `"not a callee"`},
			Args: []ast.Expr{&ast.UnaryExpr{Op: token.NOT, X: target}, &ast.Ident{Name: "other"}},
		}

		Expect(exprName(&ast.IndexListExpr{X: &ast.Ident{Name: "List"}})).To(Equal("List"))
		Expect(exprName(&ast.BasicLit{Kind: token.STRING, Value: `"literal"`})).To(BeEmpty())
		Expect(callName(call)).To(BeEmpty())
		Expect(callContainsArg(call, target)).To(BeTrue())
		Expect(callContainsFirstArg(call, target)).To(BeTrue())
		Expect(callContainsFirstArg(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}, target}}, target)).To(BeFalse())

		Expect(keyName(&ast.BasicLit{Kind: token.STRING, Value: `"message_id"`})).To(Equal("message_id"))
		Expect(keyName(&ast.BasicLit{Kind: token.STRING, Value: `"`})).To(BeEmpty())
		Expect(keyName(&ast.Ident{Name: "StatusCode"})).To(Equal("StatusCode"))

		Expect(isStringKeyedMap(nil)).To(BeFalse())
		Expect(isStringKeyedMap(types.NewMap(types.Typ[types.String], types.Typ[types.Int]))).To(BeTrue())
		Expect(isStringKeyedMap(types.NewMap(types.Typ[types.Int], types.Typ[types.String]))).To(BeFalse())
		Expect(isStringKeyedMap(types.Typ[types.String])).To(BeFalse())
	})
})
