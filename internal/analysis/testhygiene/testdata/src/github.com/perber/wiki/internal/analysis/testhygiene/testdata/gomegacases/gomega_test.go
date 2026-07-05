package gomegacases

import (
	"errors"
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
func Succeed() any                                      { return nil }
func (assertion) To(matcher any, annotations ...any)    {}
func (assertion) NotTo(matcher any, annotations ...any) {}

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
})

func returnError() error {
	return nil
}
