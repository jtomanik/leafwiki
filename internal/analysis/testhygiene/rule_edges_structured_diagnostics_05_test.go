package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go/ast"
)

var _ = ginkgo.Describe("testhygiene structured diagnostics", func() {
	ginkgo.It("reports matcher factories that gate error semantics on captured boolean state", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/cmd/leafwiki/main_edges_gomega_test.go", "github.com/perber/wiki/cmd/leafwiki", `package main

import (
	"context"
	"errors"
	"net/http"
)

type GomegaMatcher interface{}
type matcherBuilder struct{}
type gcustomPackage struct{}
type workspaceRecord struct {
	ID string
}
type descriptor struct {
	SchemaVersion int
}

var gcustom gcustomPackage

func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }
func Satisfy(fn any) GomegaMatcher { return nil }

func MatchProjectDaemonHealthError(healthy bool, target error) GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return !healthy && errors.Is(err, target), nil
	}).WithMessage("match project daemon health error")
}

func matchLeafwikiHelperStopError(ready bool) GomegaMatcher {
	return Satisfy(func(err error) bool {
		return err == nil || errors.Is(err, context.Canceled) || ready
	})
}

func MatchHomeFederatedFirstContact(home bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(workspace workspaceRecord) (bool, error) {
		return home && workspace.ID != "", nil
	}).WithMessage("match home federated workspace")
}

func MatchStaleProjectDaemonHealth(healthy bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(desc *descriptor) (bool, error) {
		return desc != nil && desc.SchemaVersion == 0 && !healthy, nil
	}).WithMessage("match stale project daemon health")
}

func matchIssuedCSRFCookie(secure bool) GomegaMatcher {
	return gcustom.MakeMatcher(func(cookie http.Cookie) (bool, error) {
		return cookie.Secure == secure, nil
	}).WithMessage("match CSRF cookie security")
}
`)
		for _, name := range []string{"MatchProjectDaemonHealthError", "matchLeafwikiHelperStopError", "MatchHomeFederatedFirstContact", "MatchStaleProjectDaemonHealth", "matchIssuedCSRFCookie"} {
			checkGomegaMatcherFactorySignature(h.ctx, h.findFunc(name))
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
			"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
			"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
			"semh:gomega.proxy-boolean: matcher factory captures boolean state while matching domain semantics; assert the semantic result directly or split into explicit domain matchers",
			"semh:gomega.proxy-boolean: matcher factory returns a predicate-only boolean oracle; assert a semantic value or compose structured Gomega matchers instead",
		))
	})

	ginkgo.It("reports matcher factories that feed raw boolean parameters into transformed expected contracts", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/http/middleware/security/csrf_cookie_test.go", "github.com/perber/wiki/internal/http/middleware/security", `package security

import "net/http"

type GomegaMatcher interface{}
type cookieContract struct {
	Name   string
	Secure bool
}

func Equal(expected any) GomegaMatcher { return nil }
func WithTransform(transform any, matcher GomegaMatcher) GomegaMatcher { return nil }

func matchIssuedCSRFCookie(name string, secure bool) GomegaMatcher {
	expected := cookieContract{Name: name, Secure: secure}
	return WithTransform(func(cookie *http.Cookie) cookieContract {
		return cookieContract{Name: cookie.Name, Secure: cookie.Secure}
	}, Equal(expected))
}
`)
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchIssuedCSRFCookie"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: matcher factory feeds raw boolean parameters into transformed expected contracts; expose semantic matcher variants or typed outcome values instead",
		))
	})

	ginkgo.It("reports matcher factories that return proxy boolean field predicates", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/workspacesync/semantic_fixture_workspacesync_test.go", "github.com/perber/wiki/internal/workspacesync", `package workspacesync

type GomegaMatcher interface{}
type matcherBuilder struct{}
type gcustomPackage struct{}
type CommitHash string
type SyncStatus struct {
	Enabled        bool
	WatcherRunning bool
	LastCommitHash CommitHash
}

var gcustom gcustomPackage

func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }

func matchEnabledWorkspaceSyncStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.Enabled, nil
	}).WithMessage("report enabled workspace sync status")
}

func matchDisabledWorkspaceSyncStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return !status.Enabled, nil
	}).WithMessage("report disabled workspace sync status")
}

func matchRunningWatcherStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.WatcherRunning, nil
	}).WithMessage("report running workspace watcher")
}

func matchCommittedStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.Enabled && status.LastCommitHash != "", nil
	}).WithMessage("report committed workspace sync status")
}
`)
		for _, name := range []string{"matchEnabledWorkspaceSyncStatus", "matchDisabledWorkspaceSyncStatus", "matchRunningWatcherStatus", "matchCommittedStatus"} {
			checkGomegaMatcherFactorySignature(h.ctx, h.findFunc(name))
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: matcher factory returns proxy boolean fields as the matcher oracle; assert a semantic value or include the domain outcome in the matcher",
			"semh:gomega.proxy-boolean: matcher factory returns proxy boolean fields as the matcher oracle; assert a semantic value or include the domain outcome in the matcher",
			"semh:gomega.proxy-boolean: matcher factory returns proxy boolean fields as the matcher oracle; assert a semantic value or include the domain outcome in the matcher",
			"semh:gomega.proxy-boolean: matcher factory returns a predicate-only boolean oracle; assert a semantic value or compose structured Gomega matchers instead",
		))
	})

	ginkgo.It("reports type-asserted map index assertion endpoints", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/http/router_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func ContainSubstring(needle string) any { return nil }

func TestRouterResponse() {
	resp := map[string]any{"content": "Root README", "status": 200}
	Expect(resp["content"].(string)).To(ContainSubstring("Root README"))
	Expect(resp["status"].(int)).To(Equal(200))
}
`)
		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
		))
	})

	ginkgo.It("reports raw MCP protocol result status assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/mcp/tools_test.go", "github.com/perber/wiki/internal/wiki/mcp", `package mcp

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }
func HaveField(name string, matcher any) any { return nil }
type Fields map[string]any
func MatchFields(_ any, fields Fields) any { return nil }
func SatisfyAll(matchers ...any) any { return nil }

type CallToolResult struct {
	IsError bool
}

func TestToolResult() {
	result := &CallToolResult{}
	Expect(result.IsError).To(BeFalse())
	Expect(result).To(HaveField("IsError", BeTrue()))
	Expect(result).To(SatisfyAll(HaveField("IsError", BeFalse())))
	Expect(result).To(MatchFields(nil, Fields{"IsError": BeFalse()}))
}
`)
		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
			"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
			"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
			"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
		))
	})

	ginkgo.It("reports matcher factories that hide raw MCP protocol result status assertions", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/mcp/tools_test.go", "github.com/perber/wiki/internal/wiki/mcp", `package mcp

type GomegaMatcher interface{}
type matcherBuilder struct{}
type gcustomPackage struct{}

var gcustom gcustomPackage

func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }

type CallToolResult struct {
	IsError bool
}

func matchToolErrorResult() GomegaMatcher {
	return gcustom.MakeMatcher(func(result *CallToolResult) (bool, error) {
		return result != nil && result.IsError, nil
	}).WithMessage("be an MCP tool error result")
}
`)
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchToolErrorResult"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.structured-protocol-status: assert MCP tool-result success or error semantics with a domain matcher instead of matching IsError as a raw boolean",
		))
	})

	ginkgo.It("reports map index aliases asserted as local values", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/http/router_test.go", "github.com/perber/wiki/internal/http", `package http

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func ContainSubstring(needle string) any { return nil }

func TestRouterResponse() {
	resp := map[string]any{"content": "Root README", "prefix": "/docs"}
	got := resp["prefix"]
	Expect(got).To(Equal("/docs"))
	content, _ := resp["content"].(string)
	Expect(content).To(ContainSubstring("Root"))
}
`)
		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
			"semh:gomega.map-index: use HaveKeyWithValue matcher instead of asserting a direct map index value",
		))
	})

	ginkgo.It("reports weak non-empty assertions on collections and semantic scalar fields", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/links/link_refactor_test.go", "github.com/perber/wiki/internal/links", `package links

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func (assertion) NotTo(matcher any, extra ...any) {}
func BeEmpty() any { return nil }
func Not(matcher any) any { return nil }

func TestLinkWarnings() {
	warnings := []string{"reference link skipped"}
	Expect(warnings).NotTo(BeEmpty())
	byPath := map[string]int{"/docs": 1}
	Expect(byPath).To(Not(BeEmpty()))
	contextOutput := struct{ ContextToken string }{}
	userRecord := struct{ User struct{ ID string } }{}
	ordinary := struct{ Title string }{}
	Expect(contextOutput.ContextToken).NotTo(BeEmpty())
	Expect(userRecord.User.ID).To(Not(BeEmpty()))
	Expect(ordinary.Title).NotTo(BeEmpty())
}
`)
		for _, call := range append(h.findCalls("To"), h.findCalls("NotTo")...) {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.non-empty-collection: assert collection contents or cardinality semantics instead of only NotTo(BeEmpty())",
			"semh:gomega.non-empty-collection: assert collection contents or cardinality semantics instead of only NotTo(BeEmpty())",
			"semh:gomega.semantic-scalar-not-empty: assert semantic scalar value meaning instead of only checking for non-empty text",
			"semh:gomega.semantic-scalar-not-empty: assert semantic scalar value meaning instead of only checking for non-empty text",
		))
	})

	ginkgo.It("reports discarded semantic boolean returns in specs", ginkgo.Label("unit"), func() {
		h := newRuleHarnessWithFiles("/repo/internal/agenthooks/agenthooks_test.go", "github.com/perber/wiki/internal/agenthooks", map[string]string{
			"/repo/internal/agenthooks/agenthooks.go": `package agenthooks

type ProviderID string
type Event struct{}

const ProviderCodex ProviderID = "codex"

func Normalize(provider ProviderID, raw []byte) (Event, bool) {
	return Event{}, true
}
`,
			"/repo/internal/agenthooks/agenthooks_test.go": `package agenthooks

type RoleHealth struct {
	PID int
}

func findRoleHealth() (RoleHealth, bool) {
	return RoleHealth{PID: 123}, true
}

func TestAgentHookNormalization() {
	_, _ = Normalize(ProviderCodex, []byte("{}"))
	initial, _ := findRoleHealth()
	_ = initial
}
`,
		})
		ast.Inspect(h.file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if ok {
				checkGomegaIgnoredSemanticBoolean(h.ctx, assign)
			}
			return true
		})

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.ignored-semantic-boolean: assert the semantic presence/status result instead of discarding a semantic boolean return with _",
			"semh:gomega.ignored-semantic-boolean: assert the semantic presence/status result instead of discarding a semantic boolean return with _",
		))
	})
})
