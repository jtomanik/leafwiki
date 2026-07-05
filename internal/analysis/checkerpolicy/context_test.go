package checkerpolicy_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/analysis/checkerpolicy"
	"golang.org/x/tools/go/analysis"
)

const ruleSemanticDirectCast checkerpolicy.RuleID = "semantic.direct-cast"
const ruleGinkgoTopLevelIt checkerpolicy.RuleID = "ginkgo.top-level-it"
const ruleGomegaEqualZero checkerpolicy.RuleID = "gomega.equal-zero"

func TestRuleSetFormatsStableRulePrefixes(t *testing.T) {
	g := NewWithT(t)
	rules := testRuleSet()

	message := rules.FormatDiagnosticMessage(checkerpolicy.Diagnostic{
		Rule:    ruleSemanticDirectCast,
		Message: "diagnostic.payload",
	})

	prefix, body, found := strings.Cut(message, ": ")
	if !found {
		t.Fatal("formatted diagnostic should include prefix separator")
	}
	g.Expect(prefix).To(Equal("semh:semantic.direct-cast"))
	g.Expect(body).To(Equal("diagnostic.payload"))
}

func TestRuleSetRegistersWaiverDiagnosticsAsHardRules(t *testing.T) {
	rules := checkerpolicy.NewRuleSet(map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{}, nil, 0)

	for _, id := range []checkerpolicy.RuleID{
		checkerpolicy.RuleWaiverBudgetExceeded,
		checkerpolicy.RuleWaiverDuplicate,
		checkerpolicy.RuleWaiverMalformed,
		checkerpolicy.RuleWaiverMissingExplanation,
		checkerpolicy.RuleWaiverNonWaivableRule,
		checkerpolicy.RuleWaiverStale,
		checkerpolicy.RuleWaiverUnknownRule,
	} {
		metadata, ok := rules.MetadataForRule(id)
		if !ok {
			t.Fatalf("missing rule metadata for %s", id)
		}
		if metadata.MessagePrefix != string(id) {
			t.Fatalf("metadata prefix for %s = %q", id, metadata.MessagePrefix)
		}
		if metadata.Waivable {
			t.Fatalf("waiver infrastructure rule %s must stay hard", id)
		}
		if metadata.Scope != checkerpolicy.WaiverScopeNone {
			t.Fatalf("waiver infrastructure rule %s has scope %v", id, metadata.Scope)
		}
	}
}

func TestContextFinalizesWaiverContract(t *testing.T) {
	tests := []struct {
		name string
		src  string
		run  func(*checkerpolicy.Context, *ast.File)
		want []string
	}{
		{
			name: "valid waiver suppresses one matching diagnostic",
			src: `package p

// semh:allow ginkgo.top-level-it -- package invariant
var _ = It("documents invariant", func() {})
`,
			run: func(ctx *checkerpolicy.Context, file *ast.File) {
				ctx.Report(ruleGinkgoTopLevelIt, findCall(file, "It"), "top-level It")
			},
			want: nil,
		},
		{
			name: "stale waiver reports stale",
			src: `package p

// semh:allow ginkgo.top-level-it -- package invariant
func ordinary() {}
`,
			run: func(*checkerpolicy.Context, *ast.File) {},
			want: []string{
				"semh:waiver.stale: semh waiver for ginkgo.top-level-it did not match any diagnostic",
			},
		},
		{
			name: "duplicate adjacent waiver reports duplicate",
			src: `package p

// semh:allow ginkgo.top-level-it -- first
// semh:allow ginkgo.top-level-it -- duplicate
var _ = It("documents invariant", func() {})
`,
			run: func(ctx *checkerpolicy.Context, file *ast.File) {
				ctx.Report(ruleGinkgoTopLevelIt, findCall(file, "It"), "top-level It")
			},
			want: []string{
				"semh:waiver.duplicate: duplicate semh waiver for ginkgo.top-level-it; one waiver can suppress one diagnostic",
			},
		},
		{
			name: "non-waivable hard rule reports non-waivable",
			src: `package p

// semh:allow semantic.direct-cast -- hard rule
func ordinary() {}
`,
			run: func(*checkerpolicy.Context, *ast.File) {},
			want: []string{
				"semh:waiver.non-waivable-rule: semh waiver for semantic.direct-cast cannot suppress hard diagnostics",
			},
		},
		{
			name: "unknown rule reports unknown",
			src: `package p

// semh:allow ginkgo.made-up -- unknown rule
func ordinary() {}
`,
			run: func(*checkerpolicy.Context, *ast.File) {},
			want: []string{
				"semh:waiver.unknown-rule: unknown semh waiver rule ginkgo.made-up",
			},
		},
		{
			name: "bare allow reports malformed",
			src: `package p

// semh:allow
func ordinary() {}
`,
			run: func(*checkerpolicy.Context, *ast.File) {},
			want: []string{
				"semh:waiver.malformed: semh:allow waiver must name exactly one rule ID before --",
			},
		},
		{
			name: "missing explanation reports missing explanation",
			src: `package p

// semh:allow ginkgo.top-level-it
func ordinary() {}
`,
			run: func(*checkerpolicy.Context, *ast.File) {},
			want: []string{
				"semh:waiver.missing-explanation: semh:allow ginkgo.top-level-it must include an explanation after --",
			},
		},
		{
			name: "used waivers over total budget report budget failure",
			src: `package p

// semh:allow gomega.equal-zero -- first
var _ = Equal(0)
// semh:allow gomega.equal-zero -- second
var _ = Equal(0)
`,
			run: func(ctx *checkerpolicy.Context, file *ast.File) {
				for _, call := range findCalls(file, "Equal") {
					ctx.Report(ruleGomegaEqualZero, call, "use BeZero matcher instead of Equal(0)")
				}
			},
			want: []string{
				"semh:waiver.budget-exceeded: total waiver budget exceeded: used 2, budget 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx, file, messages := newPolicyHarness(t, tt.src)

			tt.run(ctx, file)
			ctx.FinalizeDiagnostics()

			g.Expect(*messages).To(Equal(tt.want))
		})
	}
}

func testRuleSet() checkerpolicy.RuleSet {
	return checkerpolicy.NewRuleSet(map[checkerpolicy.RuleID]checkerpolicy.RuleMetadata{
		ruleSemanticDirectCast: checkerpolicy.HardRule(ruleSemanticDirectCast),
		ruleGinkgoTopLevelIt:   checkerpolicy.WaivableRule(ruleGinkgoTopLevelIt, checkerpolicy.WaiverScopeCall),
		ruleGomegaEqualZero:    checkerpolicy.WaivableRule(ruleGomegaEqualZero, checkerpolicy.WaiverScopeCall),
	}, map[checkerpolicy.RuleID]int{}, 1)
}

func newPolicyHarness(t *testing.T, src string) (*checkerpolicy.Context, *ast.File, *[]string) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/repo/policy_test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var messages []string
	pass := &analysis.Pass{
		Fset:  fset,
		Files: []*ast.File{file},
		Report: func(diagnostic analysis.Diagnostic) {
			messages = append(messages, diagnostic.Message)
		},
	}
	return checkerpolicy.NewContext(pass, testRuleSet(), true), file, &messages
}

func findCall(file *ast.File, name string) *ast.CallExpr {
	calls := findCalls(file, name)
	if len(calls) == 0 {
		panic("call not found: " + name)
	}
	return calls[0]
}

func findCalls(file *ast.File, name string) []*ast.CallExpr {
	var calls []*ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == name {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}
