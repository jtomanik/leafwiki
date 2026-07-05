package semantichygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
)

var _ = ginkgo.Describe("policy helpers", ginkgo.Label("unit"), func() {
	ginkgo.Describe("rule metadata", func() {
		ginkgo.It("classifies semantic-owned rules as hard diagnostics", func() {
			Expect(observeRuleMetadata(ruleDirectCast)).To(Equal(ruleMetadataObservation{
				State: ruleMetadataRegistered,
				Metadata: ruleMetadata{
					messagePrefix: string(ruleDirectCast),
					waivable:      false,
					scope:         waiverScopeNone,
				},
			}))
		})

		ginkgo.It("rejects unknown rule IDs", func() {
			Expect(observeRuleMetadata(ruleID("unknown.rule"))).To(Equal(ruleMetadataObservation{State: ruleMetadataMissing}))
		})

		ginkgo.DescribeTable("registers the checker rule taxonomy",
			func(id ruleID, waivable bool, scope waiverScopeKind) {
				Expect(observeRuleMetadata(id)).To(Equal(ruleMetadataObservation{
					State: ruleMetadataRegistered,
					Metadata: ruleMetadata{
						messagePrefix: string(id),
						waivable:      waivable,
						scope:         scope,
					},
				}), "rule %s should be registered", id)
			},
			ginkgo.Entry("semantic string leak", ruleID("semantic.string-leak"), false, waiverScopeNone),
			ginkgo.Entry("semantic direct cast", ruleID("semantic.direct-cast"), false, waiverScopeNone),
			ginkgo.Entry("semantic unchecked constructor", ruleID("semantic.unchecked-constructor"), false, waiverScopeNone),
			ginkgo.Entry("semantic fixture runtime constructor", ruleID("semantic.fixture-runtime-constructor"), false, waiverScopeNone),
			ginkgo.Entry("semantic raw signature", ruleID("semantic.raw-signature"), false, waiverScopeNone),
			ginkgo.Entry("semantic raw field", ruleID("semantic.raw-field"), false, waiverScopeNone),
			ginkgo.Entry("semantic raw primitive", ruleID("semantic.raw-primitive"), false, waiverScopeNone),
			ginkgo.Entry("semantic validator return", ruleID("semantic.validator-return"), false, waiverScopeNone),
			ginkgo.Entry("i18n raw prose", ruleID("i18n.raw-prose"), false, waiverScopeNone),
			ginkgo.Entry("i18n raw prose sink", ruleID("i18n.raw-prose-sink"), false, waiverScopeNone),
			ginkgo.Entry("i18n localized error passthrough", ruleID("i18n.localized-error-passthrough"), false, waiverScopeNone),
			ginkgo.Entry("i18n response status forward", ruleID("i18n.response-status-forward"), false, waiverScopeNone),
			ginkgo.Entry("i18n message field", ruleID("i18n.message-field"), false, waiverScopeNone),
			ginkgo.Entry("i18n message parameter", ruleID("i18n.message-parameter"), false, waiverScopeNone),
			ginkgo.Entry("contract raw literal", ruleID("contract.raw-literal"), false, waiverScopeNone),
		)
	})

	ginkgo.DescribeTable("semantic identifiers canonicalize across naming separators",
		func(input string, want string) {
			Expect(canonicalName(input)).To(Equal(want))
		},
		ginkgo.Entry("camel case", "WorkspaceID", "workspaceid"),
		ginkgo.Entry("underscore", "workspace_id", "workspaceid"),
		ginkgo.Entry("hyphen", "workspace-id", "workspaceid"),
		ginkgo.Entry("mixed delimiters", "Workspace-Source_Path", "workspacesourcepath"),
	)

	ginkgo.DescribeTable("field names expose semantic identity types from direct and contextual carriers",
		func(fieldName string, typeName string, want semanticTypeLookupObservation) {
			got, ok := semanticTypeForFieldName(fieldName, typeName)
			Expect(semanticTypeObservation(got, ok)).To(Equal(want))
		},
		ginkgo.Entry("direct workspace id", "WorkspaceID", "", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "WorkspaceID"}),
		ginkgo.Entry("direct route path plural", "route_paths", "", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "RoutePath"}),
		ginkgo.Entry("page context bare id", "ID", "PageStatus", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("node context bare id", "ID", "TreeNode", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("permalink context bare id", "ID", "PermalinkRecord", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("non-semantic field", "Name", "UserProfile", semanticTypeLookupObservation{State: semanticTypeMissing}),
	)

	ginkgo.DescribeTable("parameter names expose semantic identity types from function context",
		func(paramName string, funcName string, want semanticTypeLookupObservation) {
			got, ok := semanticTypeForParamName(paramName, funcName)
			Expect(semanticTypeObservation(got, ok)).To(Equal(want))
		},
		ginkgo.Entry("direct user id", "userID", "", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "UserID"}),
		ginkgo.Entry("workspace bare id", "id", "LoadWorkspace", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "WorkspaceID"}),
		ginkgo.Entry("revision bare id", "id", "RestoreRevision", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "RevisionID"}),
		ginkgo.Entry("user bare id", "id", "FindUser", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "UserID"}),
		ginkgo.Entry("tool bare id", "id", "RunTool", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "ToolID"}),
		ginkgo.Entry("page bare id", "id", "GetPage", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("lookup suffix resolves page identity", "id", "FindByID", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("non-semantic miss", "name", "FindProfile", semanticTypeLookupObservation{State: semanticTypeMissing}),
	)

	ginkgo.DescribeTable("bare ID contexts expose stable semantic identity types",
		func(context string, want semanticTypeLookupObservation) {
			got, ok := semanticTypeForBareIDContext(context)
			Expect(semanticTypeObservation(got, ok)).To(Equal(want))
		},
		ginkgo.Entry("api key", "LookupAPIKey", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "APIKeyID"}),
		ginkgo.Entry("workspace", "WorkspaceStatus", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "WorkspaceID"}),
		ginkgo.Entry("revision", "RevisionBackend", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "RevisionID"}),
		ginkgo.Entry("author", "AuthorProfile", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "UserID"}),
		ginkgo.Entry("tool", "ToolDescriptor", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "ToolID"}),
		ginkgo.Entry("node page context", "NodeBySlug", semanticTypeLookupObservation{State: semanticTypeResolved, Type: "PageID"}),
		ginkgo.Entry("miss", "Profile", semanticTypeLookupObservation{State: semanticTypeMissing}),
	)

	ginkgo.DescribeTable("contextual primitive names require surrounding domain hints",
		func(name string, context string, want semanticPrimitiveRecognitionState) {
			Expect(classifySemanticPrimitiveInContext(name, context)).To(Equal(want))
		},
		ginkgo.Entry("offset in search context", "offset", "SearchRequest", semanticPrimitiveRecognized),
		ginkgo.Entry("offset in page context", "offset", "PageQuery", semanticPrimitiveRecognized),
		ginkgo.Entry("offset outside hinted context", "offset", "WorkspaceRequest", semanticPrimitiveRejected),
		ginkgo.Entry("limit in revision context", "limit", "RevisionList", semanticPrimitiveRecognized),
		ginkgo.Entry("limit in page context", "limit", "PageSearch", semanticPrimitiveRecognized),
		ginkgo.Entry("asset upload byte limit", "maxBytes", "AssetUpload", semanticPrimitiveRecognized),
		ginkgo.Entry("stream byte limit", "max_bytes", "WriteStream", semanticPrimitiveRecognized),
		ginkgo.Entry("depth in tree context", "depth", "SubtreeNavigation", semanticPrimitiveRecognized),
		ginkgo.Entry("unknown primitive", "count", "SearchRequest", semanticPrimitiveRejected),
	)

	ginkgo.DescribeTable("direct primitive names expose stable scalar carriers",
		func(name string, want semanticPrimitiveRecognitionState) {
			Expect(classifySemanticPrimitive(name)).To(Equal(want))
		},
		ginkgo.Entry("depth", "Depth", semanticPrimitiveRecognized),
		ginkgo.Entry("limit", "Limit", semanticPrimitiveRecognized),
		ginkgo.Entry("offset", "offset", semanticPrimitiveRecognized),
		ginkgo.Entry("ordinary count", "count", semanticPrimitiveRejected),
	)

	ginkgo.DescribeTable("semantic names return precise domain types or a generic fallback",
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
		ginkgo.Entry("repo test boundary", "/repo/internal/wiki/page_test.go", true, false),
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

		Expect(classifyMessageBearingStruct(nil, plainStruct)).To(Equal(messageBearingStructAbsent))
		Expect(classifyMessageBearingStruct(plain, nil)).To(Equal(messageBearingStructAbsent))
		Expect(classifyMessageBearingStruct(plain, plainStruct)).To(Equal(messageBearingStructAbsent))
		Expect(classifyMessageBearingStruct(validation, validationStruct)).To(Equal(messageBearingStructPresent))
		Expect(classifyMessageBearingStruct(coded, codedStruct)).To(Equal(messageBearingStructPresent))

		Expect(classifyMessageIDField(nil, messageIDStruct)).To(Equal(messageIDFieldAbsent))
		Expect(classifyMessageIDField(messageID, nil)).To(Equal(messageIDFieldAbsent))
		Expect(classifyMessageIDField(plain, plainStruct)).To(Equal(messageIDFieldAbsent))
		Expect(classifyMessageIDField(messageID, messageIDStruct)).To(Equal(messageIDFieldPresent))
	})

	ginkgo.It("recognizes literal, assignment, and value-spec stable contract contexts", func() {
		empty := &ast.BasicLit{Kind: token.STRING, Value: `""`}
		root := &ast.BasicLit{Kind: token.STRING, Value: `"root"`}
		other := &ast.BasicLit{Kind: token.STRING, Value: `"other"`}
		malformed := &ast.BasicLit{Kind: token.STRING, Value: `"`}
		intLiteral := &ast.BasicLit{Kind: token.INT, Value: `0`}

		Expect(classifyEmptyOrRootString(empty)).To(Equal(stableStringLiteralEmptyOrRoot))
		Expect(classifyEmptyOrRootString(root)).To(Equal(stableStringLiteralEmptyOrRoot))
		Expect(classifyEmptyOrRootString(other)).To(Equal(stableStringLiteralRejected))
		Expect(classifyEmptyOrRootString(malformed)).To(Equal(stableStringLiteralRejected))
		Expect(classifyEmptyOrRootString(intLiteral)).To(Equal(stableStringLiteralRejected))

		contractLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"validation.error"`}
		plainLiteral := &ast.BasicLit{Kind: token.STRING, Value: `"plain"`}
		assign := &ast.AssignStmt{
			Lhs: []ast.Expr{&ast.Ident{Name: "errorCode"}},
			Rhs: []ast.Expr{contractLiteral},
		}
		Expect(classifyAssignStableContractContext(assign, contractLiteral)).To(Equal(stableContractContextSuggested))
		Expect(classifyAssignStableContractContext(assign, plainLiteral)).To(Equal(stableContractContextRejected))
		Expect(classifyAssignStableContractContext(&ast.AssignStmt{Rhs: []ast.Expr{contractLiteral}}, contractLiteral)).To(Equal(stableContractContextRejected))

		spec := &ast.ValueSpec{
			Names:  []*ast.Ident{{Name: "messageID"}},
			Values: []ast.Expr{contractLiteral},
		}
		Expect(classifyValueSpecStableContractContext(spec, contractLiteral)).To(Equal(stableContractContextSuggested))
		Expect(classifyValueSpecStableContractContext(spec, plainLiteral)).To(Equal(stableContractContextRejected))
		Expect(classifyValueSpecStableContractContext(&ast.ValueSpec{Values: []ast.Expr{contractLiteral}}, contractLiteral)).To(Equal(stableContractContextRejected))
	})

	ginkgo.It("classifies contract context helpers across AST shapes", func() {
		target := &ast.Ident{Name: "target"}
		call := &ast.CallExpr{
			Fun:  &ast.BasicLit{Kind: token.STRING, Value: `"not a callee"`},
			Args: []ast.Expr{&ast.UnaryExpr{Op: token.NOT, X: target}, &ast.Ident{Name: "other"}},
		}

		Expect(exprName(&ast.IndexListExpr{X: &ast.Ident{Name: "List"}})).To(Equal("List"))
		Expect(exprName(&ast.BasicLit{Kind: token.STRING, Value: `"literal"`})).To(BeEmpty())
		Expect(callName(call)).To(BeEmpty())
		Expect(classifyCallArgumentContainment(call, target)).To(Equal(callArgumentContained))
		Expect(classifyFirstCallArgumentContainment(call, target)).To(Equal(callArgumentContained))
		Expect(classifyFirstCallArgumentContainment(&ast.CallExpr{Args: []ast.Expr{&ast.Ident{Name: "other"}, target}}, target)).To(Equal(callArgumentAbsent))

		Expect(keyName(&ast.BasicLit{Kind: token.STRING, Value: `"message_id"`})).To(Equal("message_id"))
		Expect(keyName(&ast.BasicLit{Kind: token.STRING, Value: `"`})).To(BeEmpty())
		Expect(keyName(&ast.Ident{Name: "StatusCode"})).To(Equal("StatusCode"))

		Expect(classifyMapKeyType(nil)).To(Equal(mapKeyTypeOther))
		Expect(classifyMapKeyType(types.NewMap(types.Typ[types.String], types.Typ[types.Int]))).To(Equal(mapKeyTypeString))
		Expect(classifyMapKeyType(types.NewMap(types.Typ[types.Int], types.Typ[types.String]))).To(Equal(mapKeyTypeOther))
		Expect(classifyMapKeyType(types.Typ[types.String])).To(Equal(mapKeyTypeOther))
	})
})
