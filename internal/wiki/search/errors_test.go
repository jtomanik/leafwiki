package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	coresearch "github.com/perber/wiki/internal/search"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = ginkgo.Describe("search error responses", func() {
	ginkgo.It("TestRespondWithSearchError_ServiceUnavailable", func() {
		ctx, rec := ginTestContext()

		respondWithSearchError(ctx, ErrSearchUnavailable)

		Expect(rec).To(matchSearchStructuredError(http.StatusServiceUnavailable, ErrCodeSearchUnavailable))
	})

	ginkgo.It("TestRespondWithSearchError_InternalErrorIsSanitized", func() {
		ctx, rec := ginTestContext()

		respondWithSearchError(ctx, errors.New("sqlite disk I/O error"))

		Expect(rec).To(matchSearchStructuredError(http.StatusInternalServerError, ErrCodeSearchInternal))
	})

	ginkgo.It("maps search error codes to HTTP status codes", func() {
		Expect(searchErrorStatus(ErrCodeSearchUnavailable)).To(Equal(http.StatusServiceUnavailable))
		Expect(searchErrorStatus(ErrCodeSearchMissingQuery)).To(Equal(http.StatusBadRequest))
		Expect(searchErrorStatus(ErrCodeSearchInvalidOffset)).To(Equal(http.StatusBadRequest))
		Expect(searchErrorStatus(ErrCodeSearchInvalidLimit)).To(Equal(http.StatusBadRequest))
		Expect(searchErrorStatus(ErrCodeSearchInternal)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("renders explicit status errors as structured localized responses", func() {
		ctx, rec := ginTestContext()

		respondWithSearchStatusError(ctx, http.StatusBadRequest, ErrCodeSearchInvalidOffset, "ignored", "ignored")

		Expect(rec).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchInvalidOffset))
	})

	ginkgo.It("renders localized errors with their mapped status", func() {
		ctx, rec := ginTestContext()

		respondWithSearchError(ctx, sharederrors.NewLocalizedErrorFromCode(ErrCodeSearchMissingQuery, nil))

		Expect(rec).To(matchSearchStructuredError(http.StatusBadRequest, ErrCodeSearchMissingQuery))
	})
})

var _ = ginkgo.Describe("search request helpers", func() {
	ginkgo.It("requires either a query or non-empty normalized tags", func() {
		err := ValidateSearchRequest("", nil)
		Expect(err).To(testmatchers.MatchLocalizedError(ErrCodeSearchMissingQuery, sharederrors.MessageIDForCode(ErrCodeSearchMissingQuery)))

		err = ValidateSearchRequest("", []string{" ", ""})
		Expect(err).To(testmatchers.MatchLocalizedError(ErrCodeSearchMissingQuery, sharederrors.MessageIDForCode(ErrCodeSearchMissingQuery)))

		Expect(ValidateSearchRequest("docs", nil)).To(Succeed())
		Expect(ValidateSearchRequest("", []string{" go "})).To(Succeed())
	})

	ginkgo.It("normalizes tags by trimming, lowercasing, deduplicating, and dropping blanks", func() {
		Expect(normalizeTags([]string{" Go ", "go", "", "React", " REACT "})).To(Equal([]string{"go", "react"}))
	})

	ginkgo.It("splits comma-separated query tag values and drops empty parts", func() {
		Expect(splitTags(" go, react, ,testing ")).To(Equal([]string{"go", "react", "testing"}))
	})

	ginkgo.It("combines repeated query tags with comma-separated values", func() {
		ctx := ginContextForTarget("/api/search?tags=go,react&tags=testing")

		Expect(queryTags(ctx, "tags")).To(Equal([]string{"go", "react", "testing"}))
	})
})

var _ = ginkgo.Describe("search use cases", func() {
	ginkgo.It("returns search unavailable when the index dependency is nil", func() {
		uc := NewSearchUseCase(nil, nil, nil)

		out, err := uc.Execute(context.Background(), SearchInput{Query: "docs"})

		Expect(out).To(BeNil())
		Expect(err).To(Equal(ErrSearchUnavailable))
	})

	ginkgo.It("returns an empty normalized result for tags-only search without tag and tree dependencies", func() {
		uc := &SearchUseCase{}

		out, err := uc.searchByTags(nil, coresearch.ResultOffset(-3), coresearch.ResultLimit(0))

		Expect(err).NotTo(HaveOccurred())
		Expect(out.Result).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Count":     BeZero(),
			"Items":     BeEmpty(),
			"StartAt":   Equal(coresearch.ResultOffset(-3)),
			"PageSize":  Equal(coresearch.ResultLimit(0)),
			"TagFacets": BeEmpty(),
		})))
	})

	ginkgo.It("returns nil indexing status when no tracker is configured", func() {
		uc := NewGetIndexingStatusUseCase(nil)

		out := uc.Execute(context.Background())

		Expect(out.Status).To(BeNil())
	})

	ginkgo.It("returns an independent indexing status snapshot", func() {
		status := coresearch.NewIndexingStatus()
		status.Start()
		status.Success()
		uc := NewGetIndexingStatusUseCase(status)

		out := uc.Execute(context.Background())
		status.Fail()

		Expect(out.Status).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Active":  BeTrue(),
			"Indexed": Equal(1),
			"Failed":  BeZero(),
		})))
	})
})

func ginTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}

func ginContextForTarget(target string) *gin.Context {
	ginkgo.GinkgoHelper()
	ctx, _ := ginTestContext()
	ctx.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return ctx
}

func matchSearchStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}
