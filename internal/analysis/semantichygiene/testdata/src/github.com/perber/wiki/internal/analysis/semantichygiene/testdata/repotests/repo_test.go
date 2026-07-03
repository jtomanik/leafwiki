package repotests

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

type PageID string

func (id PageID) String() string {
	return string(id)
}

type RoutePath string

type UserID string

type ActorID string

func (id ActorID) String() string {
	return string(id)
}

type WebSessionID string

func (id WebSessionID) String() string {
	return string(id)
}

type ToolMessageID string

func (id ToolMessageID) String() string {
	return string(id)
}

type CommitHash string

func (hash CommitHash) String() string {
	return string(hash)
}

type ErrorCode string

type MessageID string

func (id MessageID) String() string {
	return string(id)
}

type fixtureMCPAPIKey struct {
	UserID string
}

type pageWireResponse struct {
	PageID string `json:"pageId"`
}

type bddDSL struct{}

func (bddDSL) Describe(description string, args ...any) bool { return true }

func (bddDSL) DescribeTable(description string, body any, entries ...any) bool { return true }

func (bddDSL) Context(description string, args ...any) bool { return true }

func (bddDSL) When(description string, args ...any) bool { return true }

func (bddDSL) It(description string, args ...any) bool { return true }

func (bddDSL) FIt(description string, args ...any) bool { return true }

func (bddDSL) PIt(description string, args ...any) bool { return true }

func (bddDSL) XIt(description string, args ...any) bool { return true }

func (bddDSL) BeforeEach(body func()) bool { return true }

func (bddDSL) Entry(description string, args ...any) any { return nil }

func (bddDSL) By(description string) {}

func (bddDSL) DeferCleanup(args ...any) {}

func (bddDSL) Skip(reason string) {}

func (bddDSL) GinkgoRecover() {}

func (bddDSL) Focus() any { return nil }

func (bddDSL) Pending() any { return nil }

func (bddDSL) Serial() any { return nil }

func (bddDSL) Ordered() any { return nil }

func (bddDSL) SpecPriority(priority int) any { return nil }

func (bddDSL) FlakeAttempts(attempts int) any { return nil }

type assertion struct{}

type asyncAssertion struct{}

type Gomega interface {
	Expect(actual any) assertion
}

type Fields map[string]any

func Expect(actual any, extra ...any) assertion { return assertion{} }

func ExpectWithOffset(offset int, actual any) assertion { return assertion{} }

func Equal(want any) any { return nil }

func ContainSubstring(want any) any { return nil }

func BeTrue() any { return nil }

func BeFalse() any { return nil }

func BeNil() any { return nil }

func HaveOccurred() any { return nil }

func HaveLen(want int) any { return nil }

func HavePrefix(want any) any { return nil }

func HaveSuffix(want any) any { return nil }

func MatchRegexp(want any) any { return nil }

func MatchJSON(want any) any { return nil }

func MatchFields(options any, fields Fields) any { return nil }

func HaveKeyWithValue(key any, value any) any { return nil }

func HaveField(field any, value any) any { return nil }

func ContainElement(element any) any { return nil }

func HaveHTTPStatus(status int) any { return nil }

func HaveHTTPBody(want any) any { return nil }

func HaveHTTPHeaderWithValue(name any, value any) any { return nil }

func WithTransform(transform any, matcher any) any { return nil }

func BeEquivalentTo(want any) any { return nil }

func BeNumerically(comparator string, compareTo ...any) any { return nil }

func BeTemporally(comparator string, compareTo any, threshold ...time.Duration) any { return nil }

func MatchError(want any, args ...any) any { return nil }

func Succeed() any { return nil }

func Receive() any { return nil }

func Eventually(actual any, args ...any) asyncAssertion { return asyncAssertion{} }

func Consistently(actual any, args ...any) asyncAssertion { return asyncAssertion{} }

func GinkgoHelper() {}

func SetDefaultEventuallyTimeout(timeout time.Duration) {}

func (assertion) To(matcher any, annotations ...any) {}

func (assertion) NotTo(matcher any, annotations ...any) {}

func (assertion) WithOffset(offset int) assertion { return assertion{} }

func (assertion) Error() assertion { return assertion{} }

func (asyncAssertion) Should(matcher any, annotations ...any) {}

func (asyncAssertion) ShouldNot(matcher any, annotations ...any) {}

func (asyncAssertion) WithContext(ctx any) asyncAssertion { return asyncAssertion{} }

func (asyncAssertion) WithTimeout(timeout any) asyncAssertion { return asyncAssertion{} }

type GomegaMatcher interface{}

type gomegaFormatConfig struct {
	MaxLength int
}

var format gomegaFormatConfig

type structuredError struct {
	Code      string
	MessageID string // want "semantic-looking field MessageID uses string in domain/service type structuredError; use MessageID or mark the type as a DTO boundary"
	Message   string
}

type validationFieldError struct {
	Field     string
	Code      FieldErrorCode
	MessageID MessageID
	Message   string
}

type FieldErrorCode string

func EntryDescription(description string) string {
	return description
}

func commitMessage() string {
	return "LeafWiki workspace sync"
}

type tableContractCase struct {
	wantCode    string
	wantMessage string
}

type Commit struct {
	Hash string // want "semantic-looking field Hash uses string in domain/service type Commit; use CommitHash or mark the type as a DTO boundary"
}

type behaviorRecord struct {
	Name    string
	Count   int
	Enabled bool
}

type structuredTestError struct{}

func (*structuredTestError) Error() string { return "" }

var _ = ginkgo.Describe("semantic checker allows natural BDD descriptions", func() {
	ginkgo.It("allows behavior prose without treating it as contract data", func() {
		if !strings.Contains("file mode mismatch: want 0600", "want 0600") {
			panic("mode assertion failed")
		}
	})
	ginkgo.It("allows ordinary content assertions but checks error prose", func() {
		body := "Bold link"
		Expect(FromBody(body)).To(Equal("Bold link"))
		Expect(errorMessage()).To(ContainSubstring("Page not found")) // want "raw localized prose \"Page not found\" used in test assertion code; assert a semantic code/message ID instead"
	})
	ginkgo.DescribeTable("allows table descriptions but still checks row data",
		func(code string) {},
		ginkgo.Entry("missing page", "page_not_found"), // want "raw stable contract literal \"page_not_found\" used in test assertion code; use the typed constant or semantic helper"
	)
	ginkgo.DescribeTable("allows ordinary table fixture data",
		func(input string, want string) {},
		ginkgo.Entry("stable-looking fixture", "workspace_id", "external token"),
	)
	ginkgo.DescribeTable("checks semantic data inside table case structs",
		func(tc tableContractCase) {},
		ginkgo.Entry(EntryDescription("semantic payload"), tableContractCase{
			wantCode:    "page_not_found", // want "raw stable contract literal \"page_not_found\" used in test assertion code; use the typed constant or semantic helper"
			wantMessage: "Page not found", // want "raw localized prose \"Page not found\" used in test assertion code; assert a semantic code/message ID instead"
		}),
	)
	ginkgo.It("checks git revision trailer keys as stable protocol data", func() {
		trailers := map[string]string{}
		Expect(trailers).To(HaveKeyWithValue("LeafWiki-Source", "filesystem")) // want "raw stable contract literal \"LeafWiki-Source\" used in test assertion code; use the typed constant or semantic helper"
	})

	ginkgo.It("checks derived git revision trailer values as stable protocol data", func() {
		trailers := map[string]string{}
		changed := trailers["LeafWiki-Changed-Markdown"] // want "raw stable contract literal \"LeafWiki-Changed-Markdown\" used in test assertion code; use the typed constant or semantic helper"
		Expect(changed).To(Equal("1"))
	})
})

var _ = ginkgo.It("documents a package invariant without a container", func() {}) // want "semh:ginkgo.top-level-it: top-level It reads like a migrated unit test; place it under a behavior container or waive with a specific reason"

// semh:allow ginkgo.top-level-it -- package-level invariant reads clearer without an artificial container
var _ = ginkgo.It("documents an exceptional package invariant", func() {})

// semh:allow ginkgo.top-level-it -- first budgeted package invariant
var _ = ginkgo.It("documents a first budgeted package invariant", func() {})

// semh:allow ginkgo.top-level-it -- second budgeted package invariant
var _ = ginkgo.It("documents a second budgeted package invariant", func() {})

// semh:allow ginkgo.top-level-it -- third budgeted package invariant // want "semh:waiver.budget-exceeded: waiver budget exceeded for ginkgo.top-level-it: used 4, budget 3"
var _ = ginkgo.It("documents a third budgeted package invariant", func() {})

func It(description string, args ...any) bool { return true }

var _ = It("local helper is not a Ginkgo DSL call", func() {})

var _ = ginkgo.Describe("ginkgo and gomega quality regressions", func() {
	ginkgo.Describe("git revision edge coverage", func() {})                           // want "Ginkgo node name \"git revision edge coverage\" reads like a coverage bucket; describe observable behavior instead"
	ginkgo.It("covers filesystem seam branches", func() {})                            // want "Ginkgo node name \"covers filesystem seam branches\" reads like a coverage bucket; describe observable behavior instead"
	ginkgo.It("exercises fail-fast startup validation through the exit seam", func() { // want "Ginkgo node name \"exercises fail-fast startup validation through the exit seam\" reads like a coverage bucket; describe observable behavior instead"
	})

	shared := strings.Builder{}           // want "move state initialization out of Ginkgo container body; declare variables in containers and initialize in setup nodes"
	Expect(shared.String()).To(Equal("")) // want "move Expect out of Ginkgo container body; containers should only declare specs and setup nodes" "use BeEmpty matcher instead of Equal\\(empty\\) for empty collection/string assertions"
	ginkgo.By("building the tree")        // want "move By out of Ginkgo container body; containers should only declare specs and setup nodes"
	ginkgo.Skip("not from construction")  // want "move Skip out of Ginkgo container body; containers should only declare specs and setup nodes"
	ginkgo.DeferCleanup(func() {})        // want "move DeferCleanup out of Ginkgo container body; containers should only declare specs and setup nodes"

	var rowFromSetup behaviorRecord
	ginkgo.BeforeEach(func() {
		rowFromSetup = behaviorRecord{Name: "setup", Count: 1}
	})

	ginkgo.FIt("does not commit focused specs", func() {})                            // want "do not commit focused Ginkgo specs; remove Focus/F-prefixed node"
	ginkgo.PIt("does not commit pending specs", func() {})                            // want "do not commit pending Ginkgo specs; finish or delete the spec instead"
	ginkgo.XIt("does not commit disabled specs", func() {})                           // want "do not commit pending Ginkgo specs; finish or delete the spec instead"
	ginkgo.It("does not commit focus decorators", ginkgo.Focus, func() {})            // want "do not commit focused Ginkgo specs; remove Focus/F-prefixed node"
	ginkgo.It("does not commit pending decorators", ginkgo.Pending, func() {})        // want "do not commit pending Ginkgo specs; finish or delete the spec instead"
	ginkgo.It("does not commit flake retries", ginkgo.FlakeAttempts(2), func() {})    // want "do not commit Ginkgo flake retries; fix the flake or quarantine it outside the suite"
	ginkgo.It("does not commit serial escapes", ginkgo.Serial, func() {})             // want "avoid Ginkgo Serial decorator unless the test suite policy explicitly allows it"
	ginkgo.Describe("does not commit ordered escapes", ginkgo.Ordered, func() {})     // want "avoid Ginkgo Ordered decorator unless the test suite policy explicitly allows it"
	ginkgo.It("does not commit priority escapes", ginkgo.SpecPriority(10), func() {}) // want "avoid Ginkgo SpecPriority decorator unless the test suite policy explicitly allows it"

	ginkgo.DescribeTable("uses row structs for wide entries",
		func(name string, count int, enabled bool, code string, message string) {},
		ginkgo.Entry("wide row", "name", 1, true, "code", "message"), // want "use a row struct for Ginkgo table entries with many parameters"
	)
	ginkgo.DescribeTable("does not capture setup values in entries",
		func(row behaviorRecord) {},
		ginkgo.Entry("setup row", rowFromSetup), // want "Ginkgo Entry arguments are evaluated at construction time; pass stable row data instead of setup-initialized variables"
	)

	ginkgo.It("uses context-aware async assertions", func(ctx context.Context) {
		Eventually(func() error { return nil }).Should(Succeed()) // want "propagate the spec context into Eventually/Consistently with WithContext or positional context"
		Eventually(func() error { return nil }).WithContext(ctx).Should(Succeed())
	})

	ginkgo.It("uses safe goroutine assertions and channel polling", func() {
		done := make(chan error, 1)
		go func() { // want "goroutine with assertions must defer GinkgoRecover\\(\\) or use GinkgoHelperGo"
			Expect(returnError()).To(Succeed())
			done <- nil
		}()
		<-done // want "avoid blocking channel receives in specs; use Eventually\\(\\.\\.\\.\\)\\.Should\\(Receive\\(\\.\\.\\.\\)\\) so failures surface"
		go func() {
			defer ginkgo.GinkgoRecover()
			Expect(returnError()).To(Succeed())
		}()
	})

	ginkgo.It("uses semantic matchers instead of boolean proxy assertions", func() {
		err := returnError()
		var typed *structuredTestError
		Expect(errors.As(err, &typed)).To(BeTrue())              // want "assert error type semantics with MatchError/Satisfy instead of errors.As\\(\\.\\.\\.\\) with BeTrue/BeFalse"
		Expect(os.IsNotExist(err)).To(BeTrue())                  // want "assert error semantics with MatchError instead of os.IsNotExist\\(\\.\\.\\.\\) with BeTrue/BeFalse"
		Eventually(func() bool { return true }).Should(BeTrue()) // want "poll a semantic value or assertion callback instead of Eventually/Consistently boolean results with BeTrue/BeFalse"
	})

	ginkgo.It("uses precise empty and zero matchers", func() {
		names := []string{}
		attrs := map[string]string{}
		zero := 0
		Expect(names).To(Equal([]string{}))          // want "use BeEmpty matcher instead of Equal\\(empty\\) for empty collection/string assertions"
		Expect(attrs).To(Equal(map[string]string{})) // want "use BeEmpty matcher instead of Equal\\(empty\\) for empty collection/string assertions"
		Expect("").To(Equal(""))                     // want "use BeEmpty matcher instead of Equal\\(empty\\) for empty collection/string assertions"
		Expect(zero).To(Equal(0))                    // want "use BeZero matcher instead of Equal\\(0\\) for zero-value assertions"
	})

	ginkgo.It("composes object and collection matchers", func() {
		record := behaviorRecord{Name: "created", Count: 1}
		records := []behaviorRecord{{Name: "created", Count: 1}}
		Expect(record.Name).To(Equal("created"))     // want "compose repeated field assertions on the same value into a semantic matcher or MatchFields"
		Expect(record.Count).To(Equal(1))            // want "compose repeated field assertions on the same value into a semantic matcher or MatchFields"
		Expect(records[0].Name).To(Equal("created")) // want "assert collections with ContainElement/ConsistOf/HaveExactElements instead of positional index field assertions"
	})

	ginkgo.It("restores global state next to setup", func() {
		Expect(os.Setenv("LEAFWIKI_MODE", "test")).To(Succeed()) // want "restore global state changes with DeferCleanup next to os.Setenv"
		format.MaxLength = 4000                                  // want "restore global state changes with DeferCleanup next to format.MaxLength"
		SetDefaultEventuallyTimeout(time.Second)                 // want "restore global state changes with DeferCleanup next to SetDefaultEventuallyTimeout"
	})

	ginkgo.It("uses reusable assertion helpers only when they are real matchers", func() {
		expectRepeatedShape(structuredError{})
		expectRepeatedShape(structuredError{})
	})
})

func TestRepoTestFixturesAndWireAssertionsAreAllowed(t *testing.T) {
	pageID := buildFixturePageID("page-1")
	if pageID.String() != "page-1" {
		t.Fatalf("unexpected page ID %q", pageID.String())
	}

	_ = fixtureMCPAPIKey{UserID: "user-1"}
	_ = pageWireResponse{PageID: pageID.String()}
	assertWireError("page_not_found") // want "raw stable contract literal \"page_not_found\" used in test assertion code; use the typed constant or semantic helper"
}

func TestRepoTestSemanticShortcutsAreRejected(t *testing.T) {
	rawPageID := "page-3"
	pageID := PageID(rawPageID)       // want "direct cast to semantic type PageID outside parser or boundary"
	loadInternalPage(pageID.String()) // want "semantic value PageID converted to string before internal call loadInternalPage; make the callee accept PageID"
}

func TestRepoTestGinRouteParamIsHTTPBoundary(t *testing.T) {
	pageID := PageID("page-1")
	_ = gin.Param{Key: "pageId", Value: pageID.String()}
}

func TestRepoTestContractStringOraclesAreRejected(t *testing.T) {
	assertStructuredError("page_not_found", "errors.page.not_found", "Page not found") // want "raw stable contract literal \"page_not_found\" used in test assertion code; use the typed constant or semantic helper" "raw stable contract literal \"errors.page.not_found\" used in test assertion code; use the typed constant or semantic helper" "raw localized prose \"Page not found\" used in test assertion code; assert a semantic code/message ID instead"
}

func TestRepoTestGomegaSemanticMatcherShortcutsAreRejected(t *testing.T) {
	err := errors.New("boom")
	messageFixture := "boom"
	sentinel := errors.New("sentinel")
	text := "abc"
	items := []string{"a"}
	values := map[string]int{"a": 1}
	rec := httptest.NewRecorder()
	resp := &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"X-Request-Id": []string{"abc"}}}
	count := 1
	now := time.Now()
	payload := structuredError{Code: "page_not_found", MessageID: "errors.page.not_found"}

	Expect(err.Error()).To(Equal("boom"))                        // want "assert error values with MatchError instead of matching err.Error\\(\\)"
	Expect(err.Error()).To(ContainSubstring("boom"))             // want "assert error values with MatchError instead of matching err.Error\\(\\)"
	Expect(err).To(BeNil())                                      // want "use HaveOccurred matcher instead of nil assertions on error values"
	Expect(err).NotTo(BeNil())                                   // want "use HaveOccurred matcher instead of nil assertions on error values"
	Expect(err).To(Equal(nil))                                   // want "use HaveOccurred matcher instead of nil assertions on error values"
	Expect(strings.Contains(text, "a")).To(BeTrue())             // want "use ContainSubstring matcher instead of asserting strings.Contains with BeTrue/BeFalse"
	_ = strings.Contains(err.Error(), messageFixture)            // want "assert error values with MatchError instead of matching err.Error\\(\\)"
	Expect(strings.HasPrefix(text, "a")).To(BeTrue())            // want "use HavePrefix matcher instead of asserting strings.HasPrefix with BeTrue/BeFalse"
	Expect(strings.HasSuffix(text, "c")).To(BeFalse())           // want "use HaveSuffix matcher instead of asserting strings.HasSuffix with BeTrue/BeFalse"
	Expect(regexp.MatchString("a+", text)).To(BeTrue())          // want "use MatchRegexp matcher instead of asserting regexp.MatchString with BeTrue/BeFalse"
	Expect(errors.Is(err, sentinel)).To(BeTrue())                // want "use MatchError matcher instead of asserting errors.Is with BeTrue/BeFalse"
	Expect(err).To(MatchError("boom"))                           // want "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
	Expect(err).To(MatchError(ContainSubstring("boom")))         // want "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
	Expect(err).To(MatchError(ContainSubstring(messageFixture))) // want "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
	Expect(len(items)).To(Equal(1))                              // want "use HaveLen matcher instead of asserting len\\(\\) with Equal"
	Expect(count == 1).To(BeTrue())                              // want "use semantic Gomega matchers instead of asserting binary expressions with BeTrue/BeFalse"
	Expect(count > 0).To(BeTrue())                               // want "use semantic Gomega matchers instead of asserting binary expressions with BeTrue/BeFalse"
	Expect(values["a"]).To(Equal(1))                             // want "use HaveKeyWithValue matcher instead of asserting a direct map index value"
	Expect(rec.Code).To(Equal(http.StatusOK))                    // want "use HaveHTTPStatus matcher instead of asserting response status fields directly"
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))        // want "use HaveHTTPStatus matcher instead of asserting response status fields directly"
	Expect(rec.Body.String()).To(ContainSubstring("ok"))         // want "use HaveHTTPBody matcher instead of matching recorder body strings directly"
	Expect(rec).To(HaveHTTPBody(ContainSubstring("ok")))
	Expect(rec).To(HaveHTTPBody(ContainSubstring("done")))                                                     // want "compose repeated HaveHTTPBody assertions for the same response into one matcher"
	Expect(resp.Header.Get("X-Request-Id")).To(Equal("abc"))                                                   // want "use HaveHTTPHeaderWithValue matcher instead of matching response header values directly"
	Expect(httptest.NewRequest(http.MethodGet, "/", nil)).To(HaveHTTPHeaderWithValue("X-Request-Id", "req-1")) // want "HaveHTTPHeaderWithValue matches HTTP responses; assert request headers with request-header semantics instead"
	Expect(count).To(BeEquivalentTo(int64(1)))                                                                 // want "avoid BeEquivalentTo for numeric assertions; use Equal or BeNumerically"
	Expect(now).To(Equal(now))                                                                                 // want "use BeTemporally for time.Time equality assertions"
	Expect(payload.Code).To(Equal("page_not_found"))                                                           // want "assert structured error semantics with a typed domain matcher/helper instead of matching Code directly" "raw stable contract literal \"page_not_found\" used in test assertion code; use the typed constant or semantic helper"
	Expect(payload.MessageID).To(Equal("errors.page.not_found"))                                               // want "assert structured error semantics with a typed domain matcher/helper instead of matching MessageID directly" "raw stable contract literal \"errors.page.not_found\" used in test assertion code; use the typed constant or semantic helper"
	Expect(commitMessage()).To(HavePrefix("LeafWiki initial workspace snapshot"))                              // want "raw localized prose \"LeafWiki initial workspace snapshot\" used in test assertion code; assert a semantic code/message ID instead"
	Expect(payload).To(MatchFields(nil, Fields{
		"Message": Equal("LeafWiki workspace sync"), // want "raw localized prose \"LeafWiki workspace sync\" used in test assertion code; assert a semantic code/message ID instead"
	}))

	type matcherBackedRecord struct {
		Content any
	}
	matcherValue := HavePrefix("abc").(GomegaMatcher)
	Expect(text).To(Equal(matcherValue))                                             // want "do not pass a Gomega matcher as an expected value to Equal; compose or apply the matcher directly"
	Expect(text).To(Equal(matcherBackedRecord{Content: matcherValue.(interface{})})) // want "do not pass a Gomega matcher as an expected value to Equal; compose or apply the matcher directly"

	type observedRequest struct {
		Path         string
		Body         string
		Token        string
		ActorContext string
	}
	seen := observedRequest{}
	Expect(seen).To(WithTransform(func(actual observedRequest) []string { // want "do not collapse multiple fields into a positional WithTransform assertion; use HaveField/MatchFields or a named matcher"
		return []string{actual.Path, actual.Body, actual.Token, actual.ActorContext}
	}, Equal([]string{"/mcp", "{}", "daemon-token", ""})))
	Expect([]string{seen.Path, seen.Body}).To(Equal([]string{"/mcp", "{}"})) // want "do not compare multiple fields through a positional composite assertion; use HaveField/MatchFields or a named matcher"
}

func matchMatcherBackedRecord(content GomegaMatcher) any {
	type matcherBackedRecord struct {
		Content any
	}
	return Equal(matcherBackedRecord{Content: content.(interface{})}) // want "do not pass a Gomega matcher as an expected value to Equal; compose or apply the matcher directly"
}

func TestRepoTestGomegaStructuredMatcherShortcutsAreRejected(t *testing.T) {
	payload := map[string]string{"messageId": "tools.move.success", "message": "Page moved"}
	errors := []validationFieldError{{Field: "title"}}

	Expect(payload).To(HaveKeyWithValue("messageId", typedToolMessageID().String()))           // want "assert structured protocol semantics with a typed domain matcher/helper instead of matching key messageId directly" "semantic value ToolMessageID converted to string before internal call HaveKeyWithValue; make the callee accept ToolMessageID"
	Expect(payload).To(HaveKeyWithValue("message", "Page moved"))                              // want "assert structured protocol semantics with a typed domain matcher/helper instead of matching key message directly" "raw localized prose \"Page moved\" used in test assertion code; assert a semantic code/message ID instead"
	Expect(payload).To(MatchJSON(`{"messageId":"tools.move.success","message":"Page moved"}`)) // want "assert structured protocol semantics with a typed domain matcher/helper instead of matching raw MatchJSON payload directly"
	Expect(errors).To(ContainElement(HaveField("Field", "title")))                             // want "assert structured error semantics with a typed domain matcher/helper instead of matching Field directly"
	Expect(errors).To(ContainElement(HaveField("MessageID", typedMessageID().String())))       // want "assert structured error semantics with a typed domain matcher/helper instead of matching MessageID directly" "semantic value MessageID converted to string before internal call HaveField; make the callee accept MessageID"
}

func TestRepoTestNewSemanticStringerShortcutsAreRejected(t *testing.T) {
	actorID := ActorID("actor-1")
	sessionID := WebSessionID("tab-1")
	rawActorID := "actor-2"
	rawSessionID := "tab-2"

	Expect(actorID.String()).To(Equal("actor-1"))           // want "semantic value ActorID converted to string before internal call Expect; make the callee accept ActorID"
	Expect(sessionID.String()).To(Equal("tab-1"))           // want "semantic value WebSessionID converted to string before internal call Expect; make the callee accept WebSessionID"
	Expect(ActorID(rawActorID)).To(Equal(actorID))          // want "direct cast to semantic type ActorID outside parser or boundary"
	Expect(WebSessionID(rawSessionID)).To(Equal(sessionID)) // want "direct cast to semantic type WebSessionID outside parser or boundary"
}

func TestRepoTestGomegaInlineErrorShortcutsAreRejected(t *testing.T) {
	Expect(returnError()).NotTo(HaveOccurred())       // want "use Succeed matcher for inline single-error calls instead of NotTo\\(HaveOccurred\\(\\)\\)"
	Expect(returnValueAndError()).To(Succeed())       // want "use Error\\(\\) or a captured error variable when asserting multi-return functions with HaveOccurred/Succeed"
	Expect(returnValueAndError()).To(HaveOccurred())  // want "use Error\\(\\) or a captured error variable when asserting multi-return functions with HaveOccurred/Succeed"
	Expect(returnValueAndError()).Error().To(BeNil()) // want "use HaveOccurred matcher instead of nil assertions on error values"

	err := returnError()
	Expect(err).NotTo(HaveOccurred())
}

func TestRepoTestGomegaAsyncShortcutsAreRejected(t *testing.T) {
	ch := make(chan string)
	count := 0

	Eventually(ch).ShouldNot(Receive())                                                                 // want "use Consistently\\(\\.\\.\\.\\)\\.ShouldNot\\(Receive\\(\\)\\) to prove a channel stays quiet"
	Eventually(func() error { return errors.New("boom") }).Should(MatchError("boom"))                   // want "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
	Eventually(func() error { return errors.New("boom") }).Should(MatchError(ContainSubstring("boom"))) // want "assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
	Eventually(count).Should(Equal(1))                                                                  // want "wrap bare eventually-polled values in a function so polling re-reads changing state"
	Eventually(func() int { return count }).Should(Equal(1))
	Consistently(ch).ShouldNot(Receive())
}

func TestRepoTestEventuallyCallbacksUsePassedGomega(t *testing.T) {
	Eventually(func(g Gomega) {
		Expect("ready").To(Equal("ready")) // want "use the Gomega value passed into Eventually/Consistently callbacks instead of global Expect"
	}).Should(Succeed())

	Eventually(func(g Gomega) {
		g.Expect("ready").To(Equal("ready"))
	}).Should(Succeed())
}

func buildFixturePageID(raw string) PageID {
	return PageID(raw)
}

func FromBody(raw string) string { return raw }

func errorMessage() string { return "" }

func loadInternalPage(pageID string) {}

func typedToolMessageID() ToolMessageID { return "" }

func typedMessageID() MessageID { return "" }

func assertWireError(code string) { // want "test helper assertWireError parameter code uses string for ErrorCode; use the semantic type in test helpers"
}

func assertStructuredError(code string, messageID string, message string) { // want "test helper assertStructuredError parameter code uses string for ErrorCode; use the semantic type in test helpers" "test helper assertStructuredError parameter messageID uses string for MessageID; use the semantic type in test helpers" "test helper assertStructuredError parameter message accepts rendered prose; assert MessageID/catalog semantics instead"
}

func assertStructuredResponse(payload structuredError) { // want "test assertion helper assertStructuredResponse contains Gomega assertions without GinkgoHelper, WithOffset, ExpectWithOffset, or a Gomega parameter"
	Expect(payload).To(Equal(payload))
}

func assertHelperDoesWorkBeforeReporting(payload structuredError) { // want "call GinkgoHelper\\(\\) as the first statement in assertion helper assertHelperDoesWorkBeforeReporting"
	_ = payload.Code
	GinkgoHelper()
	Expect(payload).To(Equal(payload))
}

func assertStructuredResponseWithGinkgoHelper(payload structuredError) {
	GinkgoHelper()
	Expect(payload).To(Equal(payload))
}

func expectStructuredResponseWithOffset(payload structuredError) {
	ExpectWithOffset(1, payload).To(Equal(payload))
}

func expectStructuredResponseWithGomega(g Gomega, payload structuredError) {
	g.Expect(payload).To(Equal(payload))
}

func expectRepeatedShape(payload structuredError) { // want "prefer a custom Gomega matcher for reusable assertion helper expectRepeatedShape"
	GinkgoHelper()
	Expect(payload).To(Equal(payload))
}

func HaveRawStructuredError(code string, messageID string, message string) GomegaMatcher { // want "custom matcher HaveRawStructuredError parameter code uses string for ErrorCode; use the semantic type in matcher constructors" "custom matcher HaveRawStructuredError parameter messageID uses string for MessageID; use the semantic type in matcher constructors" "custom matcher HaveRawStructuredError parameter message accepts rendered prose; assert MessageID/catalog semantics instead"
	return nil
}

func ContainFieldError(field string, code FieldErrorCode, messageID MessageID) GomegaMatcher { // want "custom matcher ContainFieldError parameter field uses string for validation field identity; use a semantic field-name type or helper constant"
	return nil
}

func HaveTypedStructuredError(code ErrorCode, messageID MessageID) GomegaMatcher {
	return nil
}

func returnError() error { return nil }

func returnValueAndError() (string, error) { return "", nil }
