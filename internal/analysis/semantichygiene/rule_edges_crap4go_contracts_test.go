package semantichygiene

import (
	"go/ast"
	"go/types"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type identifierWordsCase struct {
	input string
	want  []string
}

type nameDecisionCase struct {
	name string
	want ruleBranchDecision
}

type helperSemanticParamCase struct {
	paramName string
	funcName  string
	want      semanticTypeLookupObservation
}

type helperMessageParamCase struct {
	paramName string
	funcName  string
	want      ruleBranchDecision
}

type terminalBoundaryCase struct {
	index int
	want  ruleBranchDecision
}

type bddEntryParamObservation struct {
	State ruleBranchDecision
	Name  string
	Type  types.Type
}

type describeTableObservation struct {
	State ruleBranchDecision
	Name  string
}

type semanticCompositeFieldObservation struct {
	State     ruleBranchDecision
	FieldName string
	TypeName  string
}

var _ = ginkgo.Describe("semantic hygiene helper contracts", ginkgo.Label("unit"), func() {
	ginkgo.DescribeTable("splits helper identifiers into policy words",
		func(row identifierWordsCase) {
			Expect(identifierWords(row.input)).To(Equal(row.want))
		},
		ginkgo.Entry("camel-case semantic identity", identifierWordsCase{input: "WorkspaceID", want: []string{"workspace", "id"}}),
		ginkgo.Entry("acronym boundary before lower-case word", identifierWordsCase{input: "HTTPRequestID", want: []string{"http", "request", "id"}}),
		ginkgo.Entry("hyphen and underscore separators", identifierWordsCase{input: "workspace-source_path", want: []string{"workspace", "source", "path"}}),
		ginkgo.Entry("digit boundary before acronym", identifierWordsCase{input: "page2ID", want: []string{"page2", "id"}}),
	)

	ginkgo.DescribeTable("recognizes fixture carrier names narrowly",
		func(row nameDecisionCase) {
			Expect(decisionFor(isTestFixtureStructName(row.name))).To(Equal(row.want))
		},
		ginkgo.Entry("fixture", nameDecisionCase{name: "workspaceFixture", want: ruleBranchAccepted}),
		ginkgo.Entry("request DTO", nameDecisionCase{name: "daemonRequestDTO", want: ruleBranchAccepted}),
		ginkgo.Entry("response wire", nameDecisionCase{name: "projectResponseWire", want: ruleBranchAccepted}),
		ginkgo.Entry("ordinary domain model", nameDecisionCase{name: "WorkspaceRecord", want: ruleBranchRejected}),
	)

	ginkgo.DescribeTable("recognizes assertion subject names with semantic contracts",
		func(row nameDecisionCase) {
			Expect(decisionFor(nameSuggestsTestSubjectContract(row.name))).To(Equal(row.want))
		},
		ginkgo.Entry("body payload", nameDecisionCase{name: "body", want: ruleBranchAccepted}),
		ginkgo.Entry("error code", nameDecisionCase{name: "errorCode", want: ruleBranchAccepted}),
		ginkgo.Entry("message id", nameDecisionCase{name: "toolMessageID", want: ruleBranchAccepted}),
		ginkgo.Entry("diagnostic exception", nameDecisionCase{name: "diagnosticError", want: ruleBranchRejected}),
		ginkgo.Entry("ordinary field", nameDecisionCase{name: "displayName", want: ruleBranchRejected}),
	)

	ginkgo.DescribeTable("recognizes rendered-prose assertion subjects",
		func(row nameDecisionCase) {
			Expect(decisionFor(nameSuggestsTestRenderedProseSubject(row.name))).To(Equal(row.want))
		},
		ginkgo.Entry("err word", nameDecisionCase{name: "err", want: ruleBranchAccepted}),
		ginkgo.Entry("log word", nameDecisionCase{name: "runtimeLog", want: ruleBranchAccepted}),
		ginkgo.Entry("panic word", nameDecisionCase{name: "panicOutput", want: ruleBranchAccepted}),
		ginkgo.Entry("stderr fragment", nameDecisionCase{name: "capturedStderr", want: ruleBranchAccepted}),
		ginkgo.Entry("diagnostic exception", nameDecisionCase{name: "diagnosticMessage", want: ruleBranchRejected}),
		ginkgo.Entry("ordinary field", nameDecisionCase{name: "displayName", want: ruleBranchRejected}),
	)

	ginkgo.DescribeTable("maps raw helper parameters to semantic value types",
		func(row helperSemanticParamCase) {
			got, ok := semanticTypeForTestHelperParamName(row.paramName, row.funcName)
			Expect(semanticTypeObservation(got, ok)).To(Equal(row.want))
		},
		ginkgo.Entry("message id", helperSemanticParamCase{
			paramName: "messageID",
			funcName:  "expectStructuredError",
			want:      semanticTypeLookupObservation{State: semanticTypeResolved, Type: "MessageID"},
		}),
		ginkgo.Entry("tool name", helperSemanticParamCase{
			paramName: "toolName",
			funcName:  "haveToolResponse",
			want:      semanticTypeLookupObservation{State: semanticTypeResolved, Type: "AgentToolName"},
		}),
		ginkgo.Entry("field code", helperSemanticParamCase{
			paramName: "code",
			funcName:  "expectFieldValidation",
			want:      semanticTypeLookupObservation{State: semanticTypeResolved, Type: "FieldErrorCode"},
		}),
		ginkgo.Entry("issue code", helperSemanticParamCase{
			paramName: "code",
			funcName:  "expectValidationIssue",
			want:      semanticTypeLookupObservation{State: semanticTypeResolved, Type: "IssueCode"},
		}),
		ginkgo.Entry("generic error code", helperSemanticParamCase{
			paramName: "code",
			funcName:  "expectStructuredError",
			want:      semanticTypeLookupObservation{State: semanticTypeResolved, Type: "ErrorCode"},
		}),
		ginkgo.Entry("filename outside asset context", helperSemanticParamCase{
			paramName: "filename",
			funcName:  "expectStructuredError",
			want:      semanticTypeLookupObservation{State: semanticTypeMissing},
		}),
	)

	ginkgo.DescribeTable("recognizes helper prose parameters from function intent",
		func(row helperMessageParamCase) {
			Expect(decisionFor(testHelperMessageParamName(row.paramName, row.funcName))).To(Equal(row.want))
		},
		ginkgo.Entry("message in structured helper", helperMessageParamCase{paramName: "message", funcName: "expectStructuredError", want: ruleBranchAccepted}),
		ginkgo.Entry("message in ordinary helper", helperMessageParamCase{paramName: "message", funcName: "makeFixture", want: ruleBranchRejected}),
		ginkgo.Entry("stderr in output helper", helperMessageParamCase{paramName: "stderr", funcName: "expectFatalOutput", want: ruleBranchAccepted}),
		ginkgo.Entry("reason in ordinary helper", helperMessageParamCase{paramName: "reason", funcName: "makeFixture", want: ruleBranchRejected}),
	)

	ginkgo.It("classifies Gomega matcher literals by assertion subject and protocol keys", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type Assertion struct{}
func Expect(actual any) Assertion { return Assertion{} }
func (Assertion) To(matcher any) {}
func Equal(value any) any { return value }
func ContainSubstring(value string) any { return value }
func HaveKeyWithValue(key string, value any) any { return value }

func expectStructuredError() {
	Equal("page_not_found")
}

func spec(errorCode string, body map[string]any, renderedMessage string) {
	Expect(errorCode).To(Equal("page_not_found"))
	Expect(body).To(HaveKeyWithValue("code", "page_not_found"))
	Expect(renderedMessage).To(ContainSubstring("could not load page"))
	Expect(body).To(HaveKeyWithValue("message", "could not load page"))
}
`)
		equalCalls := h.findCalls("Equal")
		assertionLiteral := basicLiteralArg(equalCalls[1], 0)

		Expect(decisionFor(isTestAssertionLiteralContext(h.ctx, assertionLiteral))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isTestStringMatcherLiteralContext(h.ctx, assertionLiteral))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isTestAssertionMatcherContractContext(h.ctx, h.findCall("HaveKeyWithValue")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isTestLocalizedProseContractLiteralContext(h.ctx, h.findLiteral("could not load page")))).To(Equal(ruleBranchAccepted))

		checkStableLiteral(h.ctx, assertionLiteral)
		checkLocalizedProseLiteral(h.ctx, h.findLiteral("could not load page"))
		Expect(h.diagnosticMessages()).To(ContainElements(
			ContainSubstring("semh:contract.raw-literal"),
			ContainSubstring("semh:i18n.raw-prose"),
		))
	})

	ginkgo.It("maps BDD entry data to table parameters before deciding semantic contract literals", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func DescribeTable(description string, body any, entries ...any) {}
func Entry(description any, args ...any) any { return nil }

func specs() {
	DescribeTable("validation result contracts",
		func(messageID string, stderr string, plain string, _ string) {},
		Entry("reports the validation message contract", "validation.required", "plain error", "plain", "ignored"),
	)
}
`)
		entry := h.findCall("Entry")
		Expect(observeBDDEntryTableParam(h.ctx, entry, 0)).To(Equal(bddEntryParamObservation{
			State: ruleBranchAccepted,
			Name:  "messageID",
			Type:  h.ctx.pass.TypesInfo.TypeOf(basicLiteralArg(entry, 1)),
		}))
		Expect(observeBDDEntryTableParam(h.ctx, entry, 1)).To(Equal(bddEntryParamObservation{
			State: ruleBranchAccepted,
			Name:  "stderr",
			Type:  h.ctx.pass.TypesInfo.TypeOf(basicLiteralArg(entry, 2)),
		}))
		Expect(observeBDDEntryTableParam(h.ctx, entry, 3)).To(Equal(bddEntryParamObservation{
			State: ruleBranchAccepted,
			Name:  "param4",
			Type:  h.ctx.pass.TypesInfo.TypeOf(basicLiteralArg(entry, 4)),
		}))
		Expect(observeEnclosingDescribeTableCall(h.ctx, entry)).To(Equal(describeTableObservation{
			State: ruleBranchAccepted,
			Name:  "DescribeTable",
		}))

		Expect(decisionFor(isBDDContractDataLiteral(h.ctx, entry, h.findLiteral("validation.required")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isBDDRenderedProseDataLiteral(h.ctx, entry, h.findLiteral("plain error")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isBDDContractDataLiteral(h.ctx, entry, h.findLiteral("plain")))).To(Equal(ruleBranchRejected))
	})

	ginkgo.It("recognizes runtime role health wire literals through assignment and value names", func() {
		h := newRuleHarness("/repo/internal/wiki/wiki_test.go", "github.com/perber/wiki/internal/wiki", `package wiki
var runtimeHealthState = "degraded"

func build() {
	roleHealthState := "crashed"
	plain := "unknown"
	_, _ = roleHealthState, plain
}
`)
		Expect(decisionFor(isRuntimeRoleHealthWireLiteralContext(h.ctx, h.findLiteral("degraded")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isRuntimeRoleHealthWireLiteralContext(h.ctx, h.findLiteral("crashed")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isRuntimeRoleHealthWireLiteralContext(h.ctx, h.findLiteral("unknown")))).To(Equal(ruleBranchRejected))
	})

	ginkgo.It("reports semantic composite field and helper signature contracts", func() {
		composite := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
type WorkspaceID string
type Workspace struct {
	ID   WorkspaceID
	Name string
}

func build() {
	_ = Workspace{ID: "current", Name: "display"}
}
`)
		Expect(observeSemanticCompositeField(composite.ctx, composite.findLiteral("current"))).To(Equal(semanticCompositeFieldObservation{
			State:     ruleBranchAccepted,
			FieldName: "ID",
			TypeName:  "WorkspaceID",
		}))
		Expect(observeSemanticCompositeField(composite.ctx, composite.findLiteral("display"))).To(Equal(semanticCompositeFieldObservation{
			State: ruleBranchRejected,
		}))

		signatures := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
func expectStructuredError(workspaceID string, message string, field string) {}
func makeFixture(name string) {}
`)
		checkTestHelperSignature(signatures.ctx, signatures.findFunc("expectStructuredError"))
		checkTestHelperSignature(signatures.ctx, signatures.findFunc("makeFixture"))
		Expect(signatures.diagnosticMessages()).To(ContainElements(
			ContainSubstring("test helper expectStructuredError parameter workspaceID uses string"),
			ContainSubstring("parameter message accepts rendered prose"),
			ContainSubstring("parameter field uses string for validation field identity"),
		))
	})

	ginkgo.It("classifies allowed string boundary calls and reports local string leaks", func() {
		calls := newRuleHarness("/repo/internal/wiki/page_test.go", "example.com/p", `package p
import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

type response struct {
	Message string `+"`json:\"message\"`"+`
}

func assertPath(value string) {}
func wrap(err error) error { return err }
func terminalBoundary() {
	fmt.Println("done")
	err := fmt.Errorf("failed")
	_ = err
	text := fmt.Sprint("value")
	_ = text
	joined := fmt.Sprint("joined") + " suffix"
	_ = joined
	comparison := fmt.Sprint("compare") == "compare"
	_ = comparison
	_ = response{Message: fmt.Sprint("json")}
	_ = wrap(fmt.Errorf("wrapped"))
	_ = url.QueryEscape("value")
	_ = filepath.Join("a", "b")
	_ = strings.TrimSpace(" value ")
	_, _ = http.NewRequest("GET", "/", nil)
	_ = append([]string{}, "value")
	assertPath("value")
}
func returnBoundary() error { return fmt.Errorf("returned") }
func returnJoinedBoundary() string { return fmt.Sprint("returned") + " suffix" }
`)
		errorfCalls := calls.findCalls("Errorf")
		Expect(decisionFor(isAllowedTerminalStringCall(calls.ctx, errorfCalls[0]))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTerminalStringCall(calls.ctx, calls.findCall("Sprint")))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedTerminalStringCall(calls.ctx, calls.findCall("QueryEscape")))).To(Equal(ruleBranchAccepted))

		Expect(decisionFor(isAllowedTerminalCallBoundary(calls.ctx, calls.findCall("Println")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTerminalCallBoundary(calls.ctx, errorfCalls[0]))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTerminalCallBoundary(calls.ctx, errorfCalls[1]))).To(Equal(ruleBranchRejected))
		Expect(decisionFor(isAllowedTerminalCallBoundary(calls.ctx, errorfCalls[2]))).To(Equal(ruleBranchAccepted))

		sprintCalls := calls.findCalls("Sprint")
		sprintCases := []terminalBoundaryCase{
			{index: 0, want: ruleBranchRejected},
			{index: 1, want: ruleBranchRejected},
			{index: 2, want: ruleBranchRejected},
			{index: 3, want: ruleBranchAccepted},
			{index: 4, want: ruleBranchAccepted},
		}
		for _, row := range sprintCases {
			Expect(decisionFor(isAllowedTerminalCallBoundary(calls.ctx, sprintCalls[row.index]))).To(Equal(row.want))
		}

		Expect(decisionFor(isAllowedTestStringCall(calls.ctx, calls.findCall("Join")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(calls.ctx, calls.findCall("TrimSpace")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(calls.ctx, calls.findCall("NewRequest")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(calls.ctx, calls.findCall("append")))).To(Equal(ruleBranchAccepted))
		Expect(decisionFor(isAllowedTestStringCall(calls.ctx, calls.findCall("assertPath")))).To(Equal(ruleBranchAccepted))

		leak := newRuleHarness("/repo/internal/wiki/page.go", "example.com/p", `package p
type WorkspaceID string
func (id WorkspaceID) String() string { return string(id) }
func leak(id WorkspaceID) {
	var raw = id.String()
	_ = raw
		}
`)
		checkStringLeak(leak.ctx, leak.findCall("String"))
		Expect(leak.diagnosticMessages()).To(ContainElement(ContainSubstring("semantic value WorkspaceID converted to string into local raw")))
	})
})

func basicLiteralArg(call *ast.CallExpr, index int) *ast.BasicLit {
	ginkgo.GinkgoHelper()

	for argIndex, arg := range call.Args {
		if argIndex != index {
			continue
		}
		lit, ok := arg.(*ast.BasicLit)
		if ok {
			return lit
		}
		ginkgo.Fail("selected call argument should be a basic literal")
	}
	ginkgo.Fail("selected call argument should exist")
	return nil
}

func observeBDDEntryTableParam(ctx *analysisContext, entry *ast.CallExpr, dataIndex int) bddEntryParamObservation {
	name, typ, ok := bddEntryTableParam(ctx, entry, dataIndex)
	if !ok {
		return bddEntryParamObservation{State: ruleBranchRejected}
	}
	return bddEntryParamObservation{State: ruleBranchAccepted, Name: name, Type: typ}
}

func observeEnclosingDescribeTableCall(ctx *analysisContext, entry *ast.CallExpr) describeTableObservation {
	call, ok := enclosingDescribeTableCall(ctx, entry)
	if !ok {
		return describeTableObservation{State: ruleBranchRejected}
	}
	return describeTableObservation{State: ruleBranchAccepted, Name: callName(call)}
}

func observeSemanticCompositeField(ctx *analysisContext, lit *ast.BasicLit) semanticCompositeFieldObservation {
	fieldName, typeName, ok := testLiteralSemanticCompositeField(ctx, lit)
	if !ok {
		return semanticCompositeFieldObservation{State: ruleBranchRejected}
	}
	return semanticCompositeFieldObservation{
		State:     ruleBranchAccepted,
		FieldName: fieldName,
		TypeName:  typeName,
	}
}
