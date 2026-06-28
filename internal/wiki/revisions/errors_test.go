package revisions

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = ginkgo.Describe("revision errors", func() {
	ginkgo.It("TestRevisionErrorStatus_InvalidLimitIsBadRequest", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionInvalidLimit)).To(Equal(http.StatusBadRequest))
	})

	ginkgo.It("TestRevisionErrorStatus_RevisionNotFoundIsNotFound", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionNotFound)).To(Equal(http.StatusNotFound))
	})

	ginkgo.It("revisionErrorStatus falls back to an internal error for service failures", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("revision error helpers create localized not-found and blob-unavailable errors", func() {
		notFound := NewRevisionNotFoundError("missing", "missing %s", "rev-1")
		Expect(notFound.Code).To(Equal(ErrCodeRevisionNotFound))
		Expect(notFound.Args).To(Equal([]string{"rev-1"}))

		cause := errors.New("blob missing")
		blob := NewRevisionAssetBlobUnavailableError("asset.png", "page-1", "rev-1", cause)
		Expect(blob.Code).To(Equal(ErrCodeRevisionPreviewAssetBlobUnavailable))
		Expect(blob.Args).To(Equal([]string{"asset.png", "page-1", "rev-1"}))
		Expect(errors.Is(blob, cause)).To(BeTrue())
	})

	ginkgo.It("mapRevisionNotFoundError converts os.ErrNotExist and preserves other errors", func() {
		Expect(mapRevisionNotFoundError(nil, "missing", "missing")).To(Succeed())

		mapped := mapRevisionNotFoundError(os.ErrNotExist, "missing", "missing %s", "rev-1")
		var localized *sharederrors.LocalizedError
		Expect(errors.As(mapped, &localized)).To(BeTrue())
		Expect(localized.Code).To(Equal(ErrCodeRevisionNotFound))

		other := errors.New("other")
		Expect(mapRevisionNotFoundError(other, "missing", "missing")).To(MatchError(other))
	})

	ginkgo.It("respondWithRevisionError maps localized, missing, and internal errors", func() {
		gin.SetMode(gin.TestMode)

		localizedRec := httptest.NewRecorder()
		localizedCtx, _ := gin.CreateTestContext(localizedRec)
		respondWithRevisionError(localizedCtx, sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidLimit, nil, "page-1"))
		Expect(localizedRec.Code).To(Equal(http.StatusBadRequest))
		Expect(localizedRec.Body.String()).To(ContainSubstring(string(ErrCodeRevisionInvalidLimit)))

		missingRec := httptest.NewRecorder()
		missingCtx, _ := gin.CreateTestContext(missingRec)
		respondWithRevisionError(missingCtx, os.ErrNotExist)
		Expect(missingRec.Code).To(Equal(http.StatusNotFound))
		Expect(missingRec.Body.String()).To(ContainSubstring(string(ErrCodeRevisionNotFound)))

		internalRec := httptest.NewRecorder()
		internalCtx, _ := gin.CreateTestContext(internalRec)
		respondWithRevisionError(internalCtx, errors.New("boom"))
		Expect(internalRec.Code).To(Equal(http.StatusInternalServerError))
		Expect(internalRec.Body.String()).To(ContainSubstring(string(ErrCodeRevisionInternalError)))
	})
})
