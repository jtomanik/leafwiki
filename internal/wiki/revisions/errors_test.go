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
	ginkgo.It("maps invalid list limits to bad request", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionInvalidLimit)).To(Equal(http.StatusBadRequest))
	})

	ginkgo.It("maps missing revisions to not found", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionNotFound)).To(Equal(http.StatusNotFound))
	})

	ginkgo.It("revisionErrorStatus falls back to an internal error for service failures", func() {
		Expect(revisionErrorStatus(ErrCodeRevisionInternalError)).To(Equal(http.StatusInternalServerError))
	})

	ginkgo.It("revision error helpers create localized not-found and blob-unavailable errors", func() {
		notFound := NewRevisionNotFoundError("missing", "missing %s", "rev-1")
		Expect(notFound).To(MatchRevisionErrorCode(ErrCodeRevisionNotFound))
		Expect(notFound.Args).To(Equal([]string{"rev-1"}))

		cause := errors.New("blob missing")
		blob := NewRevisionAssetBlobUnavailableError("asset.png", "page-1", "rev-1", cause)
		Expect(blob).To(MatchRevisionErrorCode(ErrCodeRevisionPreviewAssetBlobUnavailable))
		Expect(blob.Args).To(Equal([]string{"asset.png", "page-1", "rev-1"}))
		Expect(blob).To(MatchError(cause))
	})

	ginkgo.It("mapRevisionNotFoundError converts os.ErrNotExist and preserves other errors", func() {
		Expect(mapRevisionNotFoundError(nil, "missing", "missing")).To(Succeed())

		mapped := mapRevisionNotFoundError(os.ErrNotExist, "missing", "missing %s", "rev-1")
		Expect(mapped).To(MatchRevisionErrorCode(ErrCodeRevisionNotFound))

		other := errors.New("other")
		Expect(mapRevisionNotFoundError(other, "missing", "missing")).To(MatchError(other))
	})

	ginkgo.It("respondWithRevisionError maps localized, missing, and internal errors", func() {
		gin.SetMode(gin.TestMode)

		localizedRec := httptest.NewRecorder()
		localizedCtx, _ := gin.CreateTestContext(localizedRec)
		respondWithRevisionError(localizedCtx, sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidLimit, nil, "page-1"))
		Expect(localizedRec).To(HaveRevisionRouteError(http.StatusBadRequest, ErrCodeRevisionInvalidLimit))

		missingRec := httptest.NewRecorder()
		missingCtx, _ := gin.CreateTestContext(missingRec)
		respondWithRevisionError(missingCtx, os.ErrNotExist)
		Expect(missingRec).To(HaveRevisionRouteError(http.StatusNotFound, ErrCodeRevisionNotFound))

		internalRec := httptest.NewRecorder()
		internalCtx, _ := gin.CreateTestContext(internalRec)
		respondWithRevisionError(internalCtx, errors.New("boom"))
		Expect(internalRec).To(HaveRevisionRouteError(http.StatusInternalServerError, ErrCodeRevisionInternalError))
	})
})
