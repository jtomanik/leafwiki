package semantichygiene

import (
	"go/ast"
	"go/token"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fileBoundaryCase struct {
	input             string
	testFile          bool
	generatedVendored bool
	persistence       bool
	edge              bool
}

type ruleMetadataRegistrationState uint8

const (
	ruleMetadataMissing ruleMetadataRegistrationState = iota
	ruleMetadataRegistered
)

type ruleMetadataObservation struct {
	State    ruleMetadataRegistrationState
	Metadata ruleMetadata
}

func observeRuleMetadata(id ruleID) ruleMetadataObservation {
	metadata, ok := metadataForRule(id)
	if !ok {
		return ruleMetadataObservation{State: ruleMetadataMissing}
	}
	return ruleMetadataObservation{State: ruleMetadataRegistered, Metadata: metadata}
}

type waiverBudgetRegistrationState uint8

const (
	waiverBudgetMissing waiverBudgetRegistrationState = iota
	waiverBudgetConfigured
)

type waiverBudgetObservation struct {
	State  waiverBudgetRegistrationState
	Budget int
}

func observeWaiverBudget(id ruleID) waiverBudgetObservation {
	budget, ok := waiverBudgetForRule(id)
	if !ok {
		return waiverBudgetObservation{State: waiverBudgetMissing}
	}
	return waiverBudgetObservation{State: waiverBudgetConfigured, Budget: budget}
}

type waiverBudgetHealth uint8

const (
	waiverBudgetAbsent waiverBudgetHealth = iota
	waiverBudgetNonPositive
	waiverBudgetPositive
)

func classifyWaiverBudget(id ruleID) waiverBudgetHealth {
	budget := observeWaiverBudget(id)
	if budget.State != waiverBudgetConfigured {
		return waiverBudgetAbsent
	}
	if budget.Budget <= 0 {
		return waiverBudgetNonPositive
	}
	return waiverBudgetPositive
}

type semanticTypeLookupState uint8

const (
	semanticTypeMissing semanticTypeLookupState = iota
	semanticTypeResolved
)

type semanticTypeLookupObservation struct {
	State semanticTypeLookupState
	Type  string
}

func semanticTypeObservation(got string, ok bool) semanticTypeLookupObservation {
	if !ok {
		return semanticTypeLookupObservation{State: semanticTypeMissing}
	}
	return semanticTypeLookupObservation{State: semanticTypeResolved, Type: got}
}

type semanticPrimitiveRecognitionState uint8

const (
	semanticPrimitiveRejected semanticPrimitiveRecognitionState = iota
	semanticPrimitiveRecognized
)

func classifySemanticPrimitiveInContext(name string, context string) semanticPrimitiveRecognitionState {
	if semanticPrimitiveNameInContext(name, context) {
		return semanticPrimitiveRecognized
	}
	return semanticPrimitiveRejected
}

func classifySemanticPrimitive(name string) semanticPrimitiveRecognitionState {
	if semanticPrimitiveName(name) {
		return semanticPrimitiveRecognized
	}
	return semanticPrimitiveRejected
}

type messageBearingStructState uint8

const (
	messageBearingStructAbsent messageBearingStructState = iota
	messageBearingStructPresent
)

func classifyMessageBearingStruct(named *types.Named, strct *types.Struct) messageBearingStructState {
	if namedStructIsMessageBearing(named, strct) {
		return messageBearingStructPresent
	}
	return messageBearingStructAbsent
}

type messageIDFieldState uint8

const (
	messageIDFieldAbsent messageIDFieldState = iota
	messageIDFieldPresent
)

func classifyMessageIDField(named *types.Named, strct *types.Struct) messageIDFieldState {
	if namedStructHasMessageID(named, strct) {
		return messageIDFieldPresent
	}
	return messageIDFieldAbsent
}

type stableStringLiteralState uint8

const (
	stableStringLiteralRejected stableStringLiteralState = iota
	stableStringLiteralEmptyOrRoot
)

func classifyEmptyOrRootString(lit *ast.BasicLit) stableStringLiteralState {
	if isEmptyOrRootString(lit) {
		return stableStringLiteralEmptyOrRoot
	}
	return stableStringLiteralRejected
}

type stableContractContextState uint8

const (
	stableContractContextRejected stableContractContextState = iota
	stableContractContextSuggested
)

func classifyAssignStableContractContext(assign *ast.AssignStmt, lit *ast.BasicLit) stableContractContextState {
	if assignStmtValueNameSuggestsStableContract(assign, lit) {
		return stableContractContextSuggested
	}
	return stableContractContextRejected
}

func classifyValueSpecStableContractContext(spec *ast.ValueSpec, lit *ast.BasicLit) stableContractContextState {
	if valueSpecNameSuggestsStableContract(spec, lit) {
		return stableContractContextSuggested
	}
	return stableContractContextRejected
}

type callArgumentContainmentState uint8

const (
	callArgumentAbsent callArgumentContainmentState = iota
	callArgumentContained
)

func classifyCallArgumentContainment(call *ast.CallExpr, target ast.Expr) callArgumentContainmentState {
	if callContainsArg(call, target) {
		return callArgumentContained
	}
	return callArgumentAbsent
}

func classifyFirstCallArgumentContainment(call *ast.CallExpr, target ast.Expr) callArgumentContainmentState {
	if callContainsFirstArg(call, target) {
		return callArgumentContained
	}
	return callArgumentAbsent
}

type mapKeyTypeState uint8

const (
	mapKeyTypeOther mapKeyTypeState = iota
	mapKeyTypeString
)

func classifyMapKeyType(typ types.Type) mapKeyTypeState {
	if isStringKeyedMap(typ) {
		return mapKeyTypeString
	}
	return mapKeyTypeOther
}

var _ = ginkgo.Describe("policy helpers", ginkgo.Label("unit"), func() {
	ginkgo.Describe("rule metadata", func() {
		ginkgo.It("classifies hard semantic rules and the first waivable BDD rule", func() {
			Expect(observeRuleMetadata(ruleDirectCast)).To(Equal(ruleMetadataObservation{
				State: ruleMetadataRegistered,
				Metadata: ruleMetadata{
					messagePrefix: string(ruleDirectCast),
					waivable:      false,
					scope:         waiverScopeNone,
				},
			}))

			Expect(observeRuleMetadata(ruleGinkgoTopLevelIt)).To(Equal(ruleMetadataObservation{
				State: ruleMetadataRegistered,
				Metadata: ruleMetadata{
					messagePrefix: string(ruleGinkgoTopLevelIt),
					waivable:      true,
					scope:         waiverScopeCall,
				},
			}))
		})

		ginkgo.It("requires every waivable rule to have an explicit positive budget", func() {
			for id, metadata := range allRuleMetadata() {
				if !metadata.waivable {
					continue
				}
				Expect(classifyWaiverBudget(id)).To(Equal(waiverBudgetPositive), "waivable rule %s should have an explicit positive budget", id)
			}
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
			ginkgo.Entry("dependency direction", ruleID("dependency.e2e-proxy-internal-import"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo focus", ruleID("ginkgo.focus"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo pending", ruleID("ginkgo.pending"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo flake attempts", ruleID("ginkgo.flake-attempts"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo restricted decorator", ruleID("ginkgo.restricted-decorator"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo taxonomy missing label", ruleID("ginkgo.taxonomy-label.missing"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo taxonomy unknown label", ruleID("ginkgo.taxonomy-label.unknown"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo taxonomy dynamic label", ruleID("ginkgo.taxonomy-label.dynamic"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo taxonomy multiple labels", ruleID("ginkgo.taxonomy-label.multiple"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo container call", ruleID("ginkgo.container-call"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo container state initialization", ruleID("ginkgo.container-state-initialization"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo entry setup value", ruleID("ginkgo.entry-setup-value"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo goroutine recover", ruleID("ginkgo.goroutine-recover"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo blocking receive", ruleID("ginkgo.blocking-receive"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo helper first", ruleID("ginkgo.helper-first"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo global state cleanup", ruleID("ginkgo.global-state-cleanup"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo BDD bucket name rule", ruleID("ginkgo.coverage-name"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo vague name", ruleID("ginkgo.vague-name"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo boolean outcome name", ruleID("ginkgo.boolean-outcome-name"), false, waiverScopeNone),
			ginkgo.Entry("gomega error string", ruleID("gomega.err-error-string"), false, waiverScopeNone),
			ginkgo.Entry("gomega raw string match error", ruleID("gomega.raw-string-match-error"), false, waiverScopeNone),
			ginkgo.Entry("gomega error nil matcher", ruleID("gomega.error-nil-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega generic HaveOccurred", ruleID("gomega.generic-have-occurred"), false, waiverScopeNone),
			ginkgo.Entry("gomega inline error succeed", ruleID("gomega.inline-error-succeed"), false, waiverScopeNone),
			ginkgo.Entry("gomega multi return error matcher", ruleID("gomega.multi-return-error-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega strings contains", ruleID("gomega.strings-contains"), false, waiverScopeNone),
			ginkgo.Entry("gomega LastError not empty", ruleID("gomega.last-error-not-empty"), false, waiverScopeNone),
			ginkgo.Entry("gomega string predicate", ruleID("gomega.string-predicate"), false, waiverScopeNone),
			ginkgo.Entry("gomega regexp match string", ruleID("gomega.regexp-match-string"), false, waiverScopeNone),
			ginkgo.Entry("gomega errors is matcher", ruleID("gomega.errors-is-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega errors as matcher", ruleID("gomega.errors-as-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega os is not exist matcher", ruleID("gomega.os-is-not-exist-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega len equal", ruleID("gomega.len-equal"), false, waiverScopeNone),
			ginkgo.Entry("gomega binary boolean", ruleID("gomega.binary-boolean"), false, waiverScopeNone),
			ginkgo.Entry("gomega boolean literal", ruleID("gomega.boolean-literal"), false, waiverScopeNone),
			ginkgo.Entry("gomega comma-ok assertion", ruleID("gomega.comma-ok-assertion"), false, waiverScopeNone),
			ginkgo.Entry("gomega ignored semantic boolean", ruleID("gomega.ignored-semantic-boolean"), false, waiverScopeNone),
			ginkgo.Entry("gomega proxy boolean", ruleID("gomega.proxy-boolean"), false, waiverScopeNone),
			ginkgo.Entry("gomega control status matcher", ruleID("gomega.control-status-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega map index", ruleID("gomega.map-index"), false, waiverScopeNone),
			ginkgo.Entry("gomega http status", ruleID("gomega.http-status"), false, waiverScopeNone),
			ginkgo.Entry("gomega http body", ruleID("gomega.http-body"), false, waiverScopeNone),
			ginkgo.Entry("gomega repeated http body", ruleID("gomega.repeated-http-body"), false, waiverScopeNone),
			ginkgo.Entry("gomega http header", ruleID("gomega.http-header"), false, waiverScopeNone),
			ginkgo.Entry("gomega structured error matcher", ruleID("gomega.structured-error-matcher"), false, waiverScopeNone),
			ginkgo.Entry("gomega structured protocol key", ruleID("gomega.structured-protocol-key"), false, waiverScopeNone),
			ginkgo.Entry("gomega structured protocol payload", ruleID("gomega.structured-protocol-payload"), false, waiverScopeNone),
			ginkgo.Entry("gomega structured protocol status", ruleID("gomega.structured-protocol-status"), false, waiverScopeNone),
			ginkgo.Entry("gomega async context", ruleID("gomega.async-context"), false, waiverScopeNone),
			ginkgo.Entry("gomega async boolean", ruleID("gomega.async-boolean"), false, waiverScopeNone),
			ginkgo.Entry("gomega async negative receive", ruleID("gomega.async-negative-receive"), false, waiverScopeNone),
			ginkgo.Entry("gomega async bare value", ruleID("gomega.async-bare-value"), false, waiverScopeNone),
			ginkgo.Entry("gomega async callback expect", ruleID("gomega.async-callback-expect"), false, waiverScopeNone),
			ginkgo.Entry("gomega helper offset", ruleID("gomega.helper-offset"), false, waiverScopeNone),
			ginkgo.Entry("ginkgo top-level It", ruleID("ginkgo.top-level-it"), true, waiverScopeCall),
			ginkgo.Entry("ginkgo wide entry", ruleID("ginkgo.wide-entry"), true, waiverScopeCall),
			ginkgo.Entry("gomega helper should be matcher", ruleID("gomega.helper-should-be-matcher"), true, waiverScopeDeclaration),
			ginkgo.Entry("gomega repeated field assertions", ruleID("gomega.repeated-field-assertions"), true, waiverScopeCall),
			ginkgo.Entry("gomega collection index assertion", ruleID("gomega.collection-index-assertion"), true, waiverScopeCall),
			ginkgo.Entry("gomega non-empty collection", ruleID("gomega.non-empty-collection"), true, waiverScopeCall),
			ginkgo.Entry("gomega semantic scalar not empty", ruleID("gomega.semantic-scalar-not-empty"), true, waiverScopeCall),
			ginkgo.Entry("gomega equal empty", ruleID("gomega.equal-empty"), true, waiverScopeCall),
			ginkgo.Entry("gomega equal zero", ruleID("gomega.equal-zero"), true, waiverScopeCall),
			ginkgo.Entry("gomega numeric equivalent", ruleID("gomega.numeric-equivalent"), true, waiverScopeCall),
			ginkgo.Entry("gomega time equal", ruleID("gomega.time-equal"), true, waiverScopeCall),
			ginkgo.Entry("gomega matcher as value", ruleID("gomega.matcher-as-value"), false, waiverScopeNone),
			ginkgo.Entry("gomega positional transform", ruleID("gomega.positional-transform"), false, waiverScopeNone),
			ginkgo.Entry("gomega positional composite assertion", ruleID("gomega.positional-composite-assertion"), false, waiverScopeNone),
		)

		ginkgo.It("uses the documented initial waiver budgets", func() {
			Expect(totalWaiverBudget).To(Equal(10))
			for _, id := range []ruleID{
				ruleID("ginkgo.top-level-it"),
				ruleID("ginkgo.wide-entry"),
				ruleID("gomega.helper-should-be-matcher"),
				ruleID("gomega.repeated-field-assertions"),
				ruleID("gomega.collection-index-assertion"),
				ruleID("gomega.non-empty-collection"),
				ruleID("gomega.semantic-scalar-not-empty"),
				ruleID("gomega.equal-empty"),
				ruleID("gomega.equal-zero"),
				ruleID("gomega.numeric-equivalent"),
				ruleID("gomega.time-equal"),
			} {
				Expect(observeWaiverBudget(id)).To(Equal(waiverBudgetObservation{
					State:  waiverBudgetConfigured,
					Budget: 3,
				}), "rule %s should use the initial per-rule budget", id)
			}
		})
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

	ginkgo.It("handles AST name, call containment, key name, and map helper edge cases", func() {
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
