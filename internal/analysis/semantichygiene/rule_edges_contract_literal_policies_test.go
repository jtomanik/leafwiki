package semantichygiene

import (
	"go/ast"
	"strconv"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("semantichygiene contract literal policies", ginkgo.Label("unit"), func() {
	ginkgo.It("classifies test contract literals in assertions and BDD table data", func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type ErrorEnvelope struct {
	Message string
}
type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func HaveKeyWithValue(key string, value any) any { return nil }
func DescribeTable(description string, body any, entries ...any) any { return nil }
func Entry(description string, args ...any) any { return nil }
func assertErrorMessage(message string) {}

func TestContracts() {
	trailers := map[string]int{"LeafWiki-Agent-Seed": 1}
	Expect(trailers["LeafWiki-Agent"]).To(Equal(1))
	Expect(ErrorEnvelope{Message: "Rendered field failure"}).To(Equal(ErrorEnvelope{}))
	Expect(map[string]string{}).To(HaveKeyWithValue("message", "Rendered matcher failure"))
	assertErrorMessage("Rendered helper failure")
}

var _ = DescribeTable("documents catalog contracts",
	func(messageID string, renderedMessage string, ordinary string) {},
	Entry("semantic row", "api.example.success", "Rendered table prose", "ordinary value"),
)
`)
		trailerLiteral := h.findLiteral("LeafWiki-Agent")
		fieldLiteral := h.findLiteral("Rendered field failure")
		matcherLiteral := h.findLiteral("Rendered matcher failure")
		helperLiteral := h.findLiteral("Rendered helper failure")
		messageIDLiteral := h.findLiteral("api.example.success")
		tableProseLiteral := h.findLiteral("Rendered table prose")
		ordinaryLiteral := h.findLiteral("ordinary value")

		Expect([]bool{
			isStableTestContractLiteral(h.ctx, trailerLiteral, unquotedLiteralValue(trailerLiteral)),
			isTestTrailerIndexLiteralContext(h.ctx, trailerLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, fieldLiteral),
			isTestRenderedProseFieldLiteral(h.ctx, h.findKeyValue("Message"), fieldLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, matcherLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, helperLiteral),
			isTestContractLiteralContext(h.ctx, messageIDLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, tableProseLiteral),
			isTestContractLiteralContext(h.ctx, ordinaryLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, ordinaryLiteral),
			isStableTestContractLiteral(h.ctx, ordinaryLiteral, " "),
		}).To(Equal([]bool{true, true, true, true, true, true, true, true, false, false, false}))
	})

	ginkgo.It("classifies runtime role health wire literals by field and naming context", func() {
		h := newRuleHarness("/repo/internal/projectdaemon/runtime_test.go", "github.com/perber/wiki/internal/projectdaemon", `package projectdaemon

type RoleHealthWire struct{ State string }

func roleHealthEnvelope(value any) {}

func collectRoleHealthWire() {
	_ = RoleHealthWire{State: "degraded"}
	roleHealthStatus := "failed"
	var runtimeHealthState = "restarting"
	roleHealthEnvelope(struct{ Health string }{Health: "ok"})
	_ = struct{ Health string }{Health: "starting"}
	ordinary := "unknown"
	_ = roleHealthStatus
	_ = runtimeHealthState
	_ = ordinary
}
`)
		Expect([]bool{
			isStableTestContractLiteral(h.ctx, h.findLiteral("degraded"), "degraded"),
			isStableTestContractLiteral(h.ctx, h.findLiteral("failed"), "failed"),
			isStableTestContractLiteral(h.ctx, h.findLiteral("restarting"), "restarting"),
			isStableTestContractLiteral(h.ctx, h.findLiteral("ok"), "ok"),
			isStableTestContractLiteral(h.ctx, h.findLiteral("starting"), "starting"),
			isStableTestContractLiteral(h.ctx, h.findLiteral("unknown"), "unknown"),
			isRuntimeRoleHealthWireFieldName("role_health"),
			isRuntimeRoleHealthWireFieldName("plain"),
		}).To(Equal([]bool{true, true, true, true, true, false, true, false}))
	})

	ginkgo.It("classifies BDD table literals from positional and keyed entry data", func() {
		h := newRuleHarness("/repo/internal/wiki/catalog_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type localizedCase struct {
	MessageID       string
	RenderedMessage string
	Ordinary        string
}

func DescribeTable(description string, body any, entries ...any) any { return nil }
func Entry(description string, args ...any) any { return nil }

var _ = DescribeTable("localized error contract",
	func(messageID string, renderedMessage string, _ string) {},
	Entry("positional row", "catalog.message", "plain rendered", "ordinary"),
	Entry("keyed row", localizedCase{
		MessageID:       "catalog.keyed",
		RenderedMessage: "keyed rendered",
		Ordinary:        "ordinary keyed",
	}),
)
`)
		positionalEntry := h.findCalls("Entry")[0]

		Expect([]bool{
			isBDDContractDataLiteral(h.ctx, positionalEntry, h.findLiteral("catalog.message")),
			isBDDRenderedProseDataLiteral(h.ctx, positionalEntry, h.findLiteral("plain rendered")),
			isBDDContractDataLiteral(h.ctx, positionalEntry, h.findLiteral("ordinary")),
			isBDDContractDataLiteral(h.ctx, h.findCalls("Entry")[1], h.findLiteral("catalog.keyed")),
			isBDDRenderedProseDataLiteral(h.ctx, h.findCalls("Entry")[1], h.findLiteral("keyed rendered")),
			isBDDRenderedProseDataLiteral(h.ctx, h.findCalls("Entry")[1], h.findLiteral("ordinary keyed")),
			bddEntryTableParamNameSucceeded(h.ctx, positionalEntry, 2),
			bddEntryTableParamNameSucceeded(h.ctx, positionalEntry, 5),
			isBDDEntryCall("FEntry"),
			isBDDEntryCall("Describe"),
			isDescribeTableCall("XDescribeTable"),
			isDescribeTableCall("Describe"),
		}).To(Equal([]bool{true, true, false, true, true, false, true, false, true, false, true, false}))
	})

	ginkgo.It("keeps contract literal allowances tied to assertion and table context", func() {
		h := newRuleHarness("/repo/internal/wiki/catalog_edges_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

type assertion struct{}
type renderedCase struct {
	Message string
}

func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(expected any) any { return nil }
func DescribeTable(description string, body any, entries ...any) any { return nil }
func Entry(description string, args ...any) any { return nil }
func Describe(description string, body any) any { return nil }

func assertMessageID() any {
	return Equal("invalid_widget")
}

func TestLiteralEdges(code string) {
	Expect(code).To(Equal("missing_widget"))
	_ = "LeafWiki-Agent"
	_ = renderedCase{Message: "Rendered but unasserted"}
	_ = Entry("description only")
	_ = Describe("not table data", "plain")
}

var _ = DescribeTable("rendered contract rows",
	func(string, renderedMessage string) {},
	Entry("unnamed row", "plain id", "rendered prose"),
)

var _ = DescribeTable("no body", "not a body", Entry("row", "plain id"))
`)
		errorCodeLiteral := h.findLiteral("missing_widget")
		helperErrorLiteral := h.findLiteral("invalid_widget")
		trailerLiteral := h.findLiteral("LeafWiki-Agent")
		unassertedRenderedLiteral := h.findLiteral("Rendered but unasserted")
		descriptionOnlyEntry := h.findCalls("Entry")[0]
		positionalEntry := h.findCalls("Entry")[1]
		noBodyEntry := h.findCalls("Entry")[2]

		Expect(observePolicyHelperDecisions(
			isStableTestContractLiteral(h.ctx, errorCodeLiteral, unquotedLiteralValue(errorCodeLiteral)),
			isStableTestContractLiteral(h.ctx, helperErrorLiteral, unquotedLiteralValue(helperErrorLiteral)),
			isStableTestContractLiteral(h.ctx, trailerLiteral, unquotedLiteralValue(trailerLiteral)),
			isTestTrailerIndexLiteralContext(h.ctx, trailerLiteral),
			isTestLocalizedProseContractLiteralContext(h.ctx, unassertedRenderedLiteral),
			isTestRenderedProseFieldLiteral(h.ctx, h.findKeyValue("Message"), unassertedRenderedLiteral),
			isBDDContractDataLiteral(h.ctx, descriptionOnlyEntry, h.findLiteral("description only")),
			isBDDContractDataLiteral(h.ctx, h.findCall("Describe"), h.findLiteral("plain")),
			isBDDRenderedProseDataLiteral(h.ctx, positionalEntry, h.findLiteral("rendered prose")),
			bddEntryTableParamNameSucceeded(h.ctx, positionalEntry, 0),
			bddEntryTableParamNameSucceeded(h.ctx, noBodyEntry, 0),
			bddEntryTableParamDisplayName(nil, 2) == "param3",
			directCallArgSucceeded(h.ctx, h.findLiteral("Rendered but unasserted")),
			directArgIndexSucceeded(&ast.CallExpr{}, h.findLiteral("Rendered but unasserted")),
			directChildWithin(h.ctx, h.findLiteral("Rendered but unasserted"), positionalEntry) != nil,
		)).To(Equal([]policyHelperDecision{
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperAccepted,
			policyHelperRejected,
			policyHelperRejected,
			policyHelperRejected,
		}))
	})
})

func bddEntryTableParamNameSucceeded(ctx *analysisContext, entry *ast.CallExpr, dataIndex int) bool {
	_, ok := bddEntryTableParamName(ctx, entry, dataIndex)
	return ok
}

func directCallArgSucceeded(ctx *analysisContext, lit *ast.BasicLit) bool {
	_, _, ok := directCallArg(ctx, lit)
	return ok
}

func directArgIndexSucceeded(call *ast.CallExpr, lit *ast.BasicLit) bool {
	_, ok := directArgIndex(call, lit)
	return ok
}

func unquotedLiteralValue(lit *ast.BasicLit) string {
	value, err := strconv.Unquote(lit.Value)
	Expect(err).To(Succeed())
	return value
}
