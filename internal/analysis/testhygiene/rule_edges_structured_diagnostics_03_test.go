package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
)

var _ = ginkgo.Describe("testhygiene structured diagnostics", func() {
	ginkgo.It("flags local test adapters that hide assertions and filesystem setup", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/tree/tree_test.go", "github.com/perber/wiki/internal/tree", `package tree

type bddDSL struct{}
type treeTestT interface {
	Fatalf(format string, args ...any)
	TempDir() string
	Helper()
}
type ginkgoTreeT struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (ginkgoTreeT) Fatalf(format string, args ...any) {}
func (ginkgoTreeT) TempDir() string { return "" }
func (ginkgoTreeT) Helper() {}
func treeSpecT() treeTestT { return ginkgoTreeT{} }

var _ = ginkgo.Describe("tree behavior", func() {
	ginkgo.It("reconstructs pages", func() {
		t := treeSpecT()
		t.Helper()
		t.Fatalf("tree did not reconstruct")
		_ = t.TempDir()
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Helper adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			"semh:ginkgo.testing-t-in-spec: avoid testing.T-like.TempDir adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("ignores ordinary logger severity methods when detecting test adapters", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/logging/logging_test.go", "github.com/perber/wiki/internal/logging", `package logging

type bddDSL struct{}
type logger interface {
	Error(message string)
	Log(message string)
}
type testLogger struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (testLogger) Error(message string) {}
func (testLogger) Log(message string) {}
func newLogger() logger { return testLogger{} }

var _ = ginkgo.Describe("logging behavior", func() {
	ginkgo.It("writes an error-level log entry", func() {
		logger := newLogger()
		logger.Error("failed")
		logger.Log("debug")
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(BeEmpty())
	})

	ginkgo.It("reports direct fail calls inside spec bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) Fail(message string) {}

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		ginkgo.Fail("brand did not format")
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)
		fail := h.findCall("Fail")
		h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Fail",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.fail-in-spec: avoid direct ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
		))
	})

	ginkgo.It("reports direct fail calls inside callbacks nested in spec bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/frontd/frontd_test.go", "github.com/perber/wiki/internal/frontd", `package frontd

import "net/http"

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) Fail(message string) {}

func NewHandler(handler http.HandlerFunc) http.Handler { return handler }

var _ = ginkgo.Describe("private handler", func() {
	ginkgo.It("rejects private requests before reaching the upstream handler", func() {
		handler := NewHandler(func(w http.ResponseWriter, req *http.Request) {
			ginkgo.Fail("upstream handler was called")
		})
		_ = handler
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)
		fail := h.findCall("Fail")
		h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Fail",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.fail-in-spec: avoid direct ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
		))
	})

	ginkgo.It("reports local failure helpers inside Ginkgo spec bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/tree/tree_test.go", "github.com/perber/wiki/internal/tree", `package tree

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func failTreeSpec(format string, args ...any) {}

var _ = ginkgo.Describe("tree behavior", func() {
	ginkgo.It("reconstructs pages", func() {
		if true {
			failTreeSpec("tree did not reconstruct")
		}
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.fail-in-spec: avoid failure helper failTreeSpec inside specs; use Gomega expectations so assertions read semantically",
		))
	})

	ginkgo.It("flags helper functions that hide explicit spec failures", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/mcp/mcp_test.go", "github.com/perber/wiki/internal/mcp", `package mcp

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoHelper() {}
func (bddDSL) Fail(message string) {}

func requiredPrivateActorContextFailure() {
	ginkgo.GinkgoHelper()
	ginkgo.Fail("private actor context did not handle the request")
}

var _ = ginkgo.Describe("private actor context", func() {
	ginkgo.It("accepts trusted private actor headers", func() {
		requiredPrivateActorContextFailure()
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)
		fail := h.findCall("Fail")
		h.ctx.pass.TypesInfo.Uses[fail.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Fail",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.fail-in-spec: avoid helper requiredPrivateActorContextFailure that calls ginkgo.Fail inside specs; use Gomega expectations so assertions read semantically",
		))
	})

	ginkgo.It("reports goroutine assertions without recovery inside Ginkgo table bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type assertion struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func Expect(actual any) assertion { return assertion{} }
func BeTrue() any { return nil }
func (assertion) To(matcher any) {}

var _ = ginkgo.DescribeTable("brand rows", func() {
	go func() {
		Expect(true).To(BeTrue())
	}()
}, ginkgo.Entry("accepted"))
`)
		checkGinkgoGoroutineAssertionRecovery(h.ctx, h.findGoStmt())

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.goroutine-recover: goroutine with assertions must defer GinkgoRecover() or use GinkgoHelperGo",
		))
	})

	ginkgo.It("reports blocking receives inside Ginkgo table bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.DescribeTable("brand rows", func() {
	ch := make(chan string)
	<-ch
}, ginkgo.Entry("accepted"))
`)
		checkGinkgoBlockingReceive(h.ctx, h.findBlockingReceive())

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.blocking-receive: avoid blocking channel receives in specs; use Eventually(...).Should(Receive(...)) so failures surface",
		))
	})

	ginkgo.It("reports async assertions without context inside Ginkgo table bodies with SpecContext", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type SpecContext struct{}
type asyncAssertion struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(SpecContext), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func Eventually(actual any, args ...any) asyncAssertion { return asyncAssertion{} }
func Equal(want any) any { return nil }
func (asyncAssertion) Should(matcher any) {}

var _ = ginkgo.DescribeTable("brand rows", func(ctx SpecContext) {
	Eventually(func() int { return 1 }).Should(Equal(1))
}, ginkgo.Entry("accepted"))
`)
		checkGomegaAsyncAssertion(h.ctx, h.findCall("Should"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.async-context: propagate the spec context into Eventually/Consistently with WithContext or positional context",
		))
	})

	ginkgo.It("flags fatal and nonfatal testing adapters used as spec assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		t.Fatal("brand did not format")
		t.Errorf("brand did not format")
		t.Error("brand did not format")
	})
})
`)
		spec := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[spec.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatal assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			"semh:ginkgo.testing-t-in-spec: avoid testing.T.Errorf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
			"semh:ginkgo.testing-t-in-spec: avoid testing.T.Error assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("ignores setup and helper failures outside runnable spec bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) BeforeEach(body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

func helper(t *testing.T) {
	t.Fatalf("helper failure")
}

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.BeforeEach(func() {
		_ = ginkgo.GinkgoT()
		t.Fatalf("setup failure")
	})
})
`)
		setup := h.findCall("BeforeEach")
		h.ctx.pass.TypesInfo.Uses[setup.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"BeforeEach",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)
		adapter := h.findCall("GinkgoT")
		h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"GinkgoT",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, setup)
		checkGinkgoSpecQualityCall(h.ctx, adapter)
		for _, call := range h.findCalls("Fatalf") {
			checkGinkgoSpecQualityCall(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(BeEmpty())
	})
})
