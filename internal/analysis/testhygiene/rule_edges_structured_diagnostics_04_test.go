package testhygiene

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("testhygiene structured diagnostics", func() {
	ginkgo.It("reports raw ginkgolinter ignore comments as hard semantic-hygiene violations", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/branding/branding_test.go", "github.com/perber/wiki/internal/branding", `package branding

// ginkgo-linter:ignore-len-assertion
func helper() {}
`)
		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:ginkgo-linter.raw-ignore: raw ginkgolinter ignore comments are not allowed; fix the generic lint or use semh waivers only for waivable semantic-hygiene rules",
		))
	})

	ginkgo.It("reports boolean literal assertions that force pass or fail", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/cmd/leafwiki/main_test.go", "github.com/perber/wiki/cmd/leafwiki", `package main

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }

func TestCLIBehavior() {
	Expect(false).To(BeTrue())
	Expect(true).To(BeFalse())
	privateURLMissing := true
	privateTokenMissing := false
	Expect(privateURLMissing || privateTokenMissing).To(BeFalse())
}
`)
		for _, call := range h.findCalls("To") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.boolean-literal: use semantic Gomega assertions instead of forcing pass/fail with boolean literals",
			"semh:gomega.boolean-literal: use semantic Gomega assertions instead of forcing pass/fail with boolean literals",
			"semh:gomega.binary-boolean: use semantic Gomega matchers instead of asserting binary expressions with BeTrue/BeFalse",
		))
	})

	ginkgo.It("reports boolean literal Equal matchers inside structured matcher values", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/pages/routes_handlers_gomega_test.go", "github.com/perber/wiki/internal/wiki/pages", `package pages

type assertion struct{}
type Fields map[string]any
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func Equal(actual any) any { return nil }
func MatchFields(options any, fields Fields) any { return nil }

type pathLookup struct {
	Exists bool
	Visible bool
}

func matchExistingRoutePathLookup() any {
	return MatchFields(nil, Fields{
		"Exists": Equal(true),
		"Visible": Equal(false),
	})
}

func TestRouteLookup() {
	Expect(pathLookup{Exists: true}).To(matchExistingRoutePathLookup())
}
`)
		for _, call := range h.findCalls("Equal") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.boolean-literal: use BeTrue/BeFalse instead of Equal(true/false) for boolean values",
			"semh:gomega.boolean-literal: use BeTrue/BeFalse instead of Equal(true/false) for boolean values",
		))
	})

	ginkgo.It("reports nested boolean matchers inside structured matcher values", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/pages/i18n_success_test.go", "github.com/perber/wiki/internal/wiki/pages", `package pages

type assertion struct{}
type Fields map[string]any
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeFalse() any { return nil }
func MatchFields(options any, fields Fields) any { return nil }

type localizedMessage struct {
	Missing bool
}

func matchResolvedMessage() any {
	return MatchFields(nil, Fields{
		"Missing": BeFalse(),
	})
}

func TestCatalogMessage() {
	Expect(localizedMessage{}).To(matchResolvedMessage())
}
`)
		for _, call := range h.findCalls("BeFalse") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
		))
	})

	ginkgo.It("flags missing-file probes hidden behind boolean transform matchers", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/wiki_test.go", "github.com/perber/wiki/internal/wiki", `package wiki

import "os"

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func WithTransform(transform any, matcher any) any { return nil }
func BeTrue() any { return nil }

func TestWikiBehavior() {
	var err error
	Expect(err).To(WithTransform(os.IsNotExist, BeTrue()))
	Expect("missing.md").To(WithTransform(func(path string) error {
		_, err := os.Stat(path)
		return err
	}, WithTransform(os.IsNotExist, BeTrue())))
}
`)
		for _, call := range h.findCalls("WithTransform") {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.os-is-not-exist-matcher: assert error semantics with MatchError instead of os.IsNotExist(...) with BeTrue/BeFalse",
			"semh:gomega.os-is-not-exist-matcher: assert error semantics with MatchError instead of os.IsNotExist(...) with BeTrue/BeFalse",
		))
	})

	ginkgo.It("reports proxy boolean assertions that hide the semantic value", ginkgo.Label("unit"), func() {
		h := newRuleHarnessWithFiles("/repo/internal/projectdaemon/agent_presence_test.go", "github.com/perber/wiki/internal/projectdaemon", map[string]string{
			"/repo/internal/projectdaemon/frontmatter.go": `package projectdaemon

type Frontmatter struct{}

func ParseFrontmatter(raw string) (Frontmatter, string, bool, error) { return Frontmatter{}, "", true, nil }
`,
			"/repo/internal/projectdaemon/agent_presence_test.go": `package projectdaemon

type assertion struct{}
func Expect(actual any) assertion { return assertion{} }
func (assertion) To(matcher any, extra ...any) {}
func BeTrue() any { return nil }
func BeFalse() any { return nil }
func Equal(actual any) any { return nil }
func HaveField(name string, matcher any) any { return nil }
type Commit struct{ Created bool }
type SessionID string
type SessionRegistry struct{}
func (SessionRegistry) Heartbeat(SessionID) bool { return true }
func (SessionRegistry) SeenSession() bool { return true }
type OAuthClient struct{}
func (OAuthClient) IsPublic() bool { return true }
func clientRedirectURIAllowed(OAuthClient, string) bool { return true }
func GetEnforcePKCE() bool { return true }

func normalize() (string, bool) { return "", true }
func foundState(found bool) string {
	if found {
		return "found"
	}
	return "absent"
}
func boolState(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
type daemonState string
const (
	daemonReady daemonState = "ready"
	daemonStopped daemonState = "stopped"
)
func isDaemonReady(raw string) bool { return raw != "" }
func daemonReadyStateFor(raw string) daemonState {
	if isDaemonReady(raw) {
		return daemonReady
	}
	return daemonStopped
}
type routingScope string
const (
	routingHome routingScope = "home routing"
	routingWorkspace routingScope = "workspace routing"
)
func routingScopeFor(home bool) routingScope {
	if home {
		return routingHome
	}
	return routingWorkspace
}

func TestAgentPresence() {
	_, ok := normalize()
	Expect(ok).To(BeTrue())
	mcpCalled := false
	Expect(mcpCalled).To(BeFalse())
	cancelInvoked := false
	Expect(cancelInvoked).To(BeFalse())
	idleCanceled := false
	Expect(idleCanceled).To(BeFalse())
	Expect(struct {
		Event string
		Found bool
	}{Event: "start", Found: ok}).To(HaveField("Found", Equal(true)))
	state := foundState(ok)
	Expect(state).To(Equal("found"))
	grantOK := ok
	grantState := foundState(grantOK)
	Expect(grantState).To(Equal("found"))
	parsed := true
	parsedState := boolState(parsed)
	Expect(parsedState).To(Equal("true"))
	Expect(daemonReadyStateFor("raw")).To(Equal(daemonReady))
	Expect(routingScopeFor(ok)).To(Equal(routingHome))

	agentEnabled := true
	Expect(agentEnabled).To(BeTrue())
	Expect(struct{ Enabled bool }{Enabled: agentEnabled}).To(HaveField("Enabled", Equal(true)))
	contentChanged := true
	Expect(contentChanged).To(BeTrue())
	revisionCreated := false
	Expect(revisionCreated).To(BeFalse())
	commit := Commit{Created: true}
	Expect(commit.Created).To(BeTrue())
	linkRemoved := true
	Expect(linkRemoved).To(BeTrue())
	fileRenamed := true
	Expect(fileRenamed).To(BeTrue())
	userSetInContext := true
	Expect(userSetInContext).To(BeTrue())
	fm, body, has, err := ParseFrontmatter("raw")
	_ = fm
	_ = body
	_ = err
	Expect(has).To(BeTrue())
	registry := SessionRegistry{}
	Expect(registry.Heartbeat(SessionID("session-1"))).To(BeTrue())
	Expect(registry.SeenSession()).To(BeFalse())
	client := OAuthClient{}
	Expect(client.IsPublic()).To(BeTrue())
	Expect(clientRedirectURIAllowed(client, "http://127.0.0.1:49152/callback")).To(BeFalse())
	Expect(GetEnforcePKCE()).To(BeTrue())
}
`,
		})
		calls := append(h.findCalls("To"), h.findCalls("foundState")...)
		calls = append(calls, h.findCalls("boolState")...)
		calls = append(calls, h.findCalls("daemonReadyStateFor")...)
		calls = append(calls, h.findCalls("routingScopeFor")...)
		for _, call := range calls {
			checkGomegaSemanticMatcher(h.ctx, call)
		}

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: assert a semantic value or domain outcome instead of proxy boolean variables with boolean matchers",
			"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
			"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
			"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
			"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
			"semh:gomega.proxy-boolean: do not convert boolean variables into string states for assertions; assert the semantic value or outcome directly",
		))
	})

	ginkgo.It("reports matcher factories that return predicate-only domain booleans", ginkgo.Label("unit"), func() {
		h := newRuleHarness("/repo/internal/wiki/oauth/service_test.go", "github.com/perber/wiki/internal/wiki/oauth", `package oauth

import "errors"

type GomegaMatcher interface{}
type matcherBuilder struct{}
type gcustomPackage struct{}
type Service struct {
	fositeConfig   any
	fositeProvider any
	store          any
}

var gcustom gcustomPackage
var target = errors.New("target")

func (gcustomPackage) MakeMatcher(fn any) matcherBuilder { return matcherBuilder{} }
func (matcherBuilder) WithMessage(message string) GomegaMatcher { return nil }
func ErrorToClass(err error) error { return err }

func haveInstalledFositeServiceComponents() GomegaMatcher {
	return gcustom.MakeMatcher(func(service *Service) (bool, error) {
		if service == nil {
			return false, nil
		}
		return service.fositeConfig != nil && service.fositeProvider != nil && service.store != nil, nil
	}).WithMessage("have installed Fosite configuration, provider, and store")
}

func matchFositeRFC6749Error() GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		if err == nil {
			return false, nil
		}
		return errors.Is(ErrorToClass(err), target), nil
	}).WithMessage("match Fosite RFC6749 error class")
}
`)
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("haveInstalledFositeServiceComponents"))
		checkGomegaMatcherFactorySignature(h.ctx, h.findFunc("matchFositeRFC6749Error"))

		Expect(h.diagnosticMessages()).To(ConsistOf(
			"semh:gomega.proxy-boolean: matcher factory returns a predicate-only boolean oracle; assert a semantic value or compose structured Gomega matchers instead",
			"semh:gomega.proxy-boolean: matcher factory returns a predicate-only boolean oracle; assert a semantic value or compose structured Gomega matchers instead",
		))
	})
})
