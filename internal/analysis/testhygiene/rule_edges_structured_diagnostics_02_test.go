package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
	"go/token"
	"go/types"
)

var _ = ginkgo.Describe("testhygiene structured diagnostics", func() {
	ginkgo.It("ignores local selector calls on an identifier named ginkgo", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/page_test.go", "github.com/perber/wiki/internal/analysis/semantichygiene/testdata/repotests", `package p

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("page behavior", func() {})
var _ = ginkgo.It("documents local DSL behavior", func() {})
`)

		checkGinkgoSpecQualityCall(h.ctx, h.findCall("It"))

		Expect(h.diagnosticMessages()).To(BeEmpty())
	})

	ginkgo.It("reports Ginkgo spec names that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("TestFormatsBrand", func() {})
})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestFormatsBrand" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo spec names that preserve Go code symbols", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wikid/wikid_helpers_test.go", "github.com/perber/wiki/internal/wikid", `package wikid

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("wikid process supervision", func() {
	ginkgo.It("Supervisor.Roles returns a snapshot of marked roles", func() {})
})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "Supervisor.Roles returns a snapshot of marked roles" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo spec names that preserve exported Go identifier fragments", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/auth/use_cases_test.go", "github.com/perber/wiki/internal/wiki/auth", `package auth

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("auth use cases", func() {
	ginkgo.It("GetUsersUseCase and GetUserByIDUseCase return public users", func() {})
})
`)
		call := h.findCall("It")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"It",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "GetUsersUseCase and GetUserByIDUseCase return public users" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports helper behavior names as vague migration residue", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wikid/wikid_helpers_test.go", "github.com/perber/wiki/internal/wikid", `package wikid

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }

var _ = ginkgo.Describe("wikid helper behavior", func() {})
`)
		call := h.findCall("Describe")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Describe",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.vague-name: Ginkgo node name "wikid helper behavior" is too vague to document behavior; describe the observable outcome instead`,
		))
	})

	ginkgo.It("reports Ginkgo container names that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }

var _ = ginkgo.Describe("TestBrandRendering", func() {})
`)
		call := h.findCall("Describe")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Describe",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestBrandRendering" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo table names that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }

var _ = ginkgo.DescribeTable("TestBrandRows", func() {})
`)
		call := h.findCall("DescribeTable")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"DescribeTable",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestBrandRows" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo entry names that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.Entry("TestAcceptedBrand", 1)
`)
		call := h.findCall("Entry")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Entry",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo entry description names that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) Entry(text any, args ...any) bool { return true }
func (bddDSL) EntryDescription(text string) string { return text }

var _ = ginkgo.Entry(ginkgo.EntryDescription("TestAcceptedBrand"), 1)
`)
		call := h.findCall("Entry")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"Entry",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports Ginkgo table entry description decorators that preserve migrated Test function names", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text any, args ...any) bool { return true }
func (bddDSL) EntryDescription(text string) string { return text }

var _ = ginkgo.DescribeTable("brand rows", func() {},
	ginkgo.EntryDescription("TestAcceptedBrand"),
	ginkgo.Entry(nil, 1),
)
`)
		call := h.findCall("DescribeTable")
		h.ctx.pass.TypesInfo.Uses[call.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"DescribeTable",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, call)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			`semh:ginkgo.test-name: Ginkgo node name "TestAcceptedBrand" preserves a migrated testing.T name; describe observable behavior instead`,
		))
	})

	ginkgo.It("reports GinkgoT adapters inside Ginkgo spec bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		_ = ginkgo.GinkgoT()
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
		adapter := h.findCall("GinkgoT")
		h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"GinkgoT",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, spec)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.testing-t-in-spec: avoid GinkgoT adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("reports GinkgoT adapters inside Ginkgo table bodies", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

type bddDSL struct{}
type fakeT struct{}
var ginkgo bddDSL
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }
func (bddDSL) GinkgoT() fakeT { return fakeT{} }

var _ = ginkgo.DescribeTable("brand rows", func() {
	_ = ginkgo.GinkgoT()
}, ginkgo.Entry("accepted"))
`)
		table := h.findCall("DescribeTable")
		h.ctx.pass.TypesInfo.Uses[table.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"DescribeTable",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)
		adapter := h.findCall("GinkgoT")
		h.ctx.pass.TypesInfo.Uses[adapter.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"GinkgoT",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, table)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.testing-t-in-spec: avoid GinkgoT adapter inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("flags fatal testing adapters used as spec assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) Describe(text string, body func()) bool { return true }
func (bddDSL) It(text string, body func()) bool { return true }

var _ = ginkgo.Describe("brand behavior", func() {
	ginkgo.It("formats the brand", func() {
		t.Fatalf("brand did not format")
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
			"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})

	ginkgo.It("flags fatal testing adapters used as table assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

import "testing"

type bddDSL struct{}
var ginkgo bddDSL
var t *testing.T
func (bddDSL) DescribeTable(text string, body func(), entries ...any) bool { return true }
func (bddDSL) Entry(text string, args ...any) bool { return true }

var _ = ginkgo.DescribeTable("brand rows", func() {
	t.Fatalf("brand did not format")
}, ginkgo.Entry("accepted"))
`)
		table := h.findCall("DescribeTable")
		h.ctx.pass.TypesInfo.Uses[table.Fun.(*ast.SelectorExpr).Sel] = types.NewFunc(
			token.NoPos,
			types.NewPackage("github.com/onsi/ginkgo/v2", "ginkgo"),
			"DescribeTable",
			types.NewSignatureType(nil, nil, nil, nil, nil, false),
		)

		checkGinkgoSpecQualityCall(h.ctx, table)

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo.testing-t-in-spec: avoid testing.T.Fatalf assertion inside Ginkgo specs; use Gomega expectations and Ginkgo helpers",
		))
	})
})
