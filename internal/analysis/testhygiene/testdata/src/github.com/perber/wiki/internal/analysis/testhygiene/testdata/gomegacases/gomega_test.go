package gomegacases

import (
	"errors"
	"net/http"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

type assertion struct{}

func Expect(actual any, extra ...any) assertion         { return assertion{} }
func Equal(want any) any                                { return nil }
func BeTrue() any                                       { return nil }
func BeNil() any                                        { return nil }
func HaveOccurred() any                                 { return nil }
func MatchError(want any, args ...any) any              { return nil }
func ContainSubstring(want any) any                     { return nil }
func HavePrefix(want any) any                           { return nil }
func HaveField(name string, matcher any) any            { return nil }
func HaveKeyWithValue(key any, value any) any           { return nil }
func MatchFields(options any, fields Fields) any        { return nil }
func Succeed() any                                      { return nil }
func (assertion) To(matcher any, annotations ...any)    {}
func (assertion) NotTo(matcher any, annotations ...any) {}

type Fields map[string]any

var IgnoreExtras any

type errorResult struct {
	LastError string
	Error     string
	Message   string
}

type syncStatusRecord struct {
	LastError       string
	LastErrorDetail string
}

type daemonRoleRecord struct {
	Error string
}

type PageID string
type RevisionID string
type Slug string

type actorContract struct {
	ID         string
	Subject    string
	AuthMethod string
}

type pageContract struct {
	ID   string
	Slug string
}

type commandResult struct {
	ExitCode int
}

var _ = ginkgo.Describe("Gomega policy", ginkgo.Label("unit"), func() {
	ginkgo.It("uses semantic error assertions", func() {
		err := errors.New("boom")
		Expect(err.Error()).To(Equal("boom"))             // want "semh:gomega.err-error-string: assert error values with MatchError instead of matching err.Error\\(\\)"
		Expect(err).To(BeNil())                           // want "semh:gomega.error-nil-matcher: use HaveOccurred matcher instead of nil assertions on error values"
		Expect(err).To(HaveOccurred())                    // want "semh:gomega.generic-have-occurred: assert expected error semantics with MatchError or a domain matcher instead of generic HaveOccurred"
		Expect(err).To(MatchError("boom"))                // want "semh:gomega.raw-string-match-error: assert error semantics with a typed/domain matcher or injected error value instead of raw string MatchError"
		Expect(returnError()).NotTo(HaveOccurred())       // want "semh:gomega.inline-error-succeed: use Succeed matcher for inline single-error calls instead of NotTo\\(HaveOccurred\\(\\)\\)"
		Expect(strings.Contains("abc", "a")).To(BeTrue()) // want "semh:gomega.strings-contains: use ContainSubstring matcher instead of asserting strings.Contains with BeTrue/BeFalse"
	})

	ginkgo.It("uses collection and scalar matchers", func() {
		items := []string{"a"}
		zero := 0
		exitCode := 1
		Expect(len(items)).To(Equal(1)) // want "semh:gomega.len-equal: use HaveLen or a collection matcher instead of asserting len\\(\\) directly"
		Expect(zero).To(Equal(0))       // want "semh:gomega.equal-zero: use BeZero matcher instead of Equal\\(0\\) for zero-value assertions"
		Expect(exitCode).To(Equal(1))   // want "semh:gomega.raw-status-code: assert process/domain status with a semantic matcher or named status value instead of raw numeric status codes"
	})

	ginkgo.It("uses domain matchers for rendered and semantic matcher-tree contracts", func() {
		errorState := errorResult{LastError: "restart limit", Error: "failed", Message: "rendered"}
		status := syncStatusRecord{LastError: "primary", LastErrorDetail: "detail"}
		role := daemonRoleRecord{Error: "exit status 2"}
		errorPayload := map[string]any{"lastError": "restart limit", "lastErrorDetail": "detail", "Message": "rendered"}
		actor := actorContract{ID: "public-viewer", Subject: "user:public-viewer", AuthMethod: "api_key"}
		payload := map[string]any{"slug": "newpage", "status": float64(http.StatusOK)}
		result := commandResult{ExitCode: 1}

		Expect(errorState).To(HaveField("LastError", Equal("restart limit")))                // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching LastError directly"
		Expect(errorState).To(MatchFields(IgnoreExtras, Fields{"Error": Equal("boom")}))     // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching Error directly"
		Expect(errorState).To(HaveField("Message", Equal("rendered")))                       // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching Message directly"
		Expect(role).To(HaveField("Error", Equal("exit status 2")))                          // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching Error directly"
		Expect(status.LastError).To(Equal("primary"))                                        // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching LastError directly"
		Expect(status).To(MatchFields(IgnoreExtras, Fields{"LastError": Equal("primary")}))  // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching LastError directly"
		Expect(errorPayload).To(HaveKeyWithValue("lastError", "restart limit"))              // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching lastError directly"
		Expect(errorPayload).To(HaveKeyWithValue("lastErrorDetail", status.LastErrorDetail)) // want "semh:gomega.structured-error-matcher: assert structured error semantics with a typed domain matcher/helper instead of matching lastErrorDetail directly"
		Expect(errorPayload).To(HaveKeyWithValue("Message", "rendered"))                     // want "semh:gomega.structured-protocol-key: assert structured protocol semantics with a typed domain matcher/helper instead of matching key Message directly"

		Expect(actor).To(HaveField("ID", Equal("public-viewer")))           // want "semh:gomega.semantic-contract-probe: assert semantic field/key ID with a typed domain matcher/helper instead of raw contract values"
		Expect(actor).To(HaveField("Subject", Equal("user:public-viewer"))) // want "semh:gomega.semantic-contract-probe: assert semantic field/key Subject with a typed domain matcher/helper instead of raw contract values"
		Expect(actor).To(HaveField("AuthMethod", HavePrefix("api")))        // want "semh:gomega.semantic-contract-probe: assert semantic field/key AuthMethod with a typed domain matcher/helper instead of raw contract values"
		Expect(payload).To(HaveKeyWithValue("slug", "newpage"))             // want "semh:gomega.semantic-contract-probe: assert semantic field/key slug with a typed domain matcher/helper instead of raw contract values"
		Expect(actor).To(HaveField("ID", Equal(actor.ID)))
		Expect(result).To(HaveField("ExitCode", Equal(1)))                     // want "semh:gomega.raw-status-code: assert process/domain status with a semantic matcher or named status value instead of raw numeric status codes"
		Expect(payload).To(HaveKeyWithValue("status", float64(http.StatusOK))) // want "semh:gomega.raw-status-code: assert process/domain status with a semantic matcher or named status value instead of raw numeric status codes"
	})

	ginkgo.It("allows typed semantic fixture values in matcher-tree contracts", func() {
		actor := actorContract{ID: "public-viewer"}
		page := pageContract{ID: "readme-page", Slug: "plans"}

		Expect(actor).To(HaveField("ID", Equal(actor.ID)))
		Expect(page).To(HaveField("ID", Equal(newFixturePageID("readme-page"))))
		Expect(page).To(HaveField("ID", Equal(newFixtureRevisionID("rev3"))))
		Expect(page).To(HaveField("Slug", Equal(newFixtureSlug("plans"))))
	})
})

func returnError() error {
	return nil
}

func newFixturePageID(value string) PageID {
	return PageID(value)
}

func newFixtureRevisionID(value string) RevisionID {
	return RevisionID(value)
}

func newFixtureSlug(value string) Slug {
	return Slug(value)
}
