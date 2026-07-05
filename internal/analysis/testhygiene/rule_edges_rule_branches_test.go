package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene rule branches", ginkgo.Label("unit"), func() {
	ginkgo.It("reports LastError assertions that only prove non-empty rendered text", func() {
		h := newRuleHarness("/repo/internal/workspacesync/service_test.go", "github.com/perber/wiki/internal/workspacesync", `package workspacesync

type SyncStatus struct {
	LastError string
}

type GomegaMatcher interface{}
type assertion struct{}
type matcher interface {
	Match(any) (bool, error)
}
type matcherBuilder struct{}
type gcustomPackage struct{}

func Expect(actual any) assertion { return assertion{} }
func (assertion) NotTo(matcher any, extras ...any) {}
func BeEmpty() any { return nil }
var gcustom gcustomPackage
func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }

func TestSyncStatus() {
	status := SyncStatus{LastError: "writeback failed"}
	Expect(status.LastError).NotTo(BeEmpty())
}

func matchWatcherFactoryFailureStatus() GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return status.LastError != "", nil
	}).WithMessage("report watcher factory failure status")
}

func matchStoppedWatcherError(lastError matcher) GomegaMatcher {
	return gcustom.MakeMatcher(func(status SyncStatus) (bool, error) {
		return lastError.Match(status.LastError)
	}).WithMessage("report stopped watcher error")
}
`)

		checkGomegaSemanticMatcher(h.ctx, h.findCall("NotTo"))
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchWatcherFactoryFailureStatus"))
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchStoppedWatcherError"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
			"semh:gomega.last-error-not-empty: assert specific LastError semantics instead of only checking for non-empty rendered text",
		))
	})
})
