package pages

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"

	"github.com/gin-gonic/gin"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/localization"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

func MatchPageLocalizedCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func pagesTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-pages-test-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func requireReadmeMarkdownPathFallbackInput(path string, kind tree.NodeKind) (ReadmeMarkdownPathFallbackInput, error) {
	input, handled, err := NormalizeReadmeMarkdownPathFallbackInput(path, kind)
	if err != nil {
		return input, err
	}
	if !handled {
		return input, errors.New("README fallback input was not handled")
	}
	return input, nil
}

func requireReadmeMarkdownPathFallbackRawInput(path string, kind string) (ReadmeMarkdownPathFallbackInput, error) {
	input, handled, err := NormalizeReadmeMarkdownPathFallbackRawInput(path, kind)
	if err != nil {
		return input, err
	}
	if !handled {
		return input, errors.New("README fallback input was not handled")
	}
	return input, nil
}

func requireReadmeMarkdownPathFallback(path string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, error) {
	out, handled, err := FindReadmeMarkdownPathFallback(path, kind, lookup)
	if err != nil {
		return out, err
	}
	if !handled {
		return out, errors.New("README fallback was not handled")
	}
	return out, nil
}

func requireReadmeMarkdownPathFallbackRaw(rawPath string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, error) {
	out, handled, err := FindReadmeMarkdownPathFallbackRawInput(rawPath, pageNodeKindWireValue(kind), lookup)
	if err != nil {
		return out, err
	}
	if !handled {
		return out, errors.New("README fallback was not handled")
	}
	return out, nil
}

func ignoreReadmeMarkdownPathFallback(path string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) error {
	out, handled, err := FindReadmeMarkdownPathFallback(path, kind, lookup)
	if err != nil {
		return err
	}
	if handled || out != nil {
		return errors.New("README fallback was handled")
	}
	return nil
}

func BeRejectedMarkdownFence() types.GomegaMatcher {
	return WithTransform(markdownFenceParseStateFor, Equal(markdownLineRejected))
}

func BeRejectedMarkdownHeading() types.GomegaMatcher {
	return WithTransform(markdownHeadingParseStateFor, Equal(markdownLineRejected))
}

type markdownLineParseState uint8

const (
	markdownLineRejected markdownLineParseState = iota
	markdownLineAccepted
)

func markdownFenceParseStateFor(line string) markdownLineParseState {
	_, _, matched := parseMarkdownFence(line)
	return markdownLineParseStateFor(matched)
}

func markdownHeadingParseStateFor(line string) markdownLineParseState {
	_, _, matched := parseMarkdownHeading(line)
	return markdownLineParseStateFor(matched)
}

func markdownLineParseStateFor(matched bool) markdownLineParseState {
	if matched {
		return markdownLineAccepted
	}
	return markdownLineRejected
}

func HaveReadmeMarkdownFallbackRoutes(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return WithTransform(readmeMarkdownFallbackRouteObservationFor, Equal(readmeMarkdownFallbackRouteObservation{
		State:        readmeMarkdownFallbackRouteMatched,
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}))
}

func BeIgnoredByReadmeMarkdownFallbackRoutes() types.GomegaMatcher {
	return WithTransform(readmeMarkdownFallbackRouteObservationFor, Equal(readmeMarkdownFallbackRouteObservation{
		State: readmeMarkdownFallbackRouteIgnored,
	}))
}

type readmeMarkdownFallbackRouteState uint8

const (
	readmeMarkdownFallbackRouteIgnored readmeMarkdownFallbackRouteState = iota
	readmeMarkdownFallbackRouteMatched
)

type readmeMarkdownFallbackRouteObservation struct {
	State        readmeMarkdownFallbackRouteState
	PageRoute    string
	SectionRoute string
}

func readmeMarkdownFallbackRouteObservationFor(path string) readmeMarkdownFallbackRouteObservation {
	pageRoute, sectionRoute, matched := ReadmeMarkdownPathFallbackRoutes(path)
	if !matched {
		return readmeMarkdownFallbackRouteObservation{State: readmeMarkdownFallbackRouteIgnored}
	}
	return readmeMarkdownFallbackRouteObservation{
		State:        readmeMarkdownFallbackRouteMatched,
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}
}

func ResolveCatalogMessage() types.GomegaMatcher {
	return WithTransform(catalogMessageResolutionFor, Equal(catalogMessageResolved))
}

type catalogMessageResolution uint8

const (
	catalogMessageMissing catalogMessageResolution = iota
	catalogMessageResolved
)

func catalogMessageResolutionFor(messageID sharederrors.MessageID) catalogMessageResolution {
	rendered := localization.English.Render(messageID, "")
	if rendered.Missing || rendered.Err != nil {
		return catalogMessageMissing
	}
	return catalogMessageResolved
}

func HaveExistingRoutePathLookup(path tree.RoutePath) types.GomegaMatcher {
	return WithTransform(routePathLookupStateFor, Equal(routePathLookupState{
		Path:  path,
		State: routePathLookupExisting,
	}))
}

type routePathLookupExistence uint8

const (
	routePathLookupMissing routePathLookupExistence = iota
	routePathLookupExisting
)

type routePathLookupState struct {
	Path  tree.RoutePath
	State routePathLookupExistence
}

func routePathLookupStateFor(lookup tree.PathLookup) routePathLookupState {
	state := routePathLookupMissing
	if lookup.Exists {
		state = routePathLookupExisting
	}
	return routePathLookupState{
		Path:  lookup.Path,
		State: state,
	}
}

func HavePageOnlyReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return WithTransform(readmeMarkdownFallbackAttemptObservationFor, Equal(readmeMarkdownFallbackAttemptObservation{
		State:        readmeMarkdownFallbackAttemptPageOnly,
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}))
}

func HavePageAndSectionReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return WithTransform(readmeMarkdownFallbackAttemptObservationFor, Equal(readmeMarkdownFallbackAttemptObservation{
		State:        readmeMarkdownFallbackAttemptPageAndSection,
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}))
}

func HaveSectionReadmeMarkdownFallback(pageRoute string, sectionRoute string) types.GomegaMatcher {
	return WithTransform(readmeMarkdownFallbackAttemptObservationFor, Equal(readmeMarkdownFallbackAttemptObservation{
		State:        readmeMarkdownFallbackAttemptSectionOnly,
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}))
}

type readmeMarkdownFallbackAttemptState uint8

const (
	readmeMarkdownFallbackAttemptNone readmeMarkdownFallbackAttemptState = iota
	readmeMarkdownFallbackAttemptPageOnly
	readmeMarkdownFallbackAttemptSectionOnly
	readmeMarkdownFallbackAttemptPageAndSection
)

type readmeMarkdownFallbackAttemptObservation struct {
	State        readmeMarkdownFallbackAttemptState
	PageRoute    string
	SectionRoute string
}

func readmeMarkdownFallbackAttemptObservationFor(input ReadmeMarkdownPathFallbackInput) readmeMarkdownFallbackAttemptObservation {
	state := readmeMarkdownFallbackAttemptNone
	switch {
	case input.TryPage && input.TrySection:
		state = readmeMarkdownFallbackAttemptPageAndSection
	case input.TryPage:
		state = readmeMarkdownFallbackAttemptPageOnly
	case input.TrySection:
		state = readmeMarkdownFallbackAttemptSectionOnly
	}
	return readmeMarkdownFallbackAttemptObservation{
		State:        state,
		PageRoute:    input.PageRoute,
		SectionRoute: input.SectionRoute,
	}
}

func ReadmeFallbackSection(rootDir string, rawSectionRoute string) readmeFallbackSectionProbe {
	return readmeFallbackSectionProbe{RootDir: rootDir, RawSectionRoute: rawSectionRoute}
}

func HaveActiveReadmeFallbackSection() types.GomegaMatcher {
	return WithTransform(readmeFallbackSectionStateFor, Equal(readmeFallbackSectionActive))
}

func HaveInactiveReadmeFallbackSection() types.GomegaMatcher {
	return WithTransform(readmeFallbackSectionStateFor, Equal(readmeFallbackSectionInactive))
}

type readmeFallbackSectionState uint8

const (
	readmeFallbackSectionInactive readmeFallbackSectionState = iota
	readmeFallbackSectionActive
)

type readmeFallbackSectionProbe struct {
	RootDir         string
	RawSectionRoute string
}

func readmeFallbackSectionStateFor(probe readmeFallbackSectionProbe) readmeFallbackSectionState {
	if ReadmeFallbackSectionIsActive(probe.RootDir, probe.RawSectionRoute) {
		return readmeFallbackSectionActive
	}
	return readmeFallbackSectionInactive
}

func HavePageSaveContentChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Content", Equal(pageSaveChangePresent)))
}

func HavePageSaveSlugChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Slug", Equal(pageSaveChangePresent)))
}

func HavePageSaveTitleChange() types.GomegaMatcher {
	return WithTransform(pageSaveChangeSetFor, HaveField("Title", Equal(pageSaveChangePresent)))
}

type pageSaveChangeState uint8

const (
	pageSaveChangeAbsent pageSaveChangeState = iota
	pageSaveChangePresent
)

type pageSaveChangeSet struct {
	Content pageSaveChangeState
	Slug    pageSaveChangeState
	Title   pageSaveChangeState
}

func pageSaveChangeSetFor(event pagesave.PageSaveEvent) pageSaveChangeSet {
	return pageSaveChangeSet{
		Content: pageSaveChangeStateFor(event.ContentChanged),
		Slug:    pageSaveChangeStateFor(event.SlugChanged),
		Title:   pageSaveChangeStateFor(event.TitleChanged),
	}
}

func pageSaveChangeStateFor(changed bool) pageSaveChangeState {
	if changed {
		return pageSaveChangePresent
	}
	return pageSaveChangeAbsent
}

func HavePageErrorDetail(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return WithTransform(pageErrorDetailObservationFor, Equal(pageErrorDetailObservation{
		Resolution: pageErrorDetailResolved,
		Status:     status,
		Code:       code,
		MessageID:  sharederrors.MessageIDForCode(code),
	}))
}

func BeIgnoredByPageErrorDetail() types.GomegaMatcher {
	return WithTransform(pageErrorDetailObservationFor, Equal(pageErrorDetailObservation{
		Resolution: pageErrorDetailIgnored,
	}))
}

type pageErrorDetailResolution uint8

const (
	pageErrorDetailIgnored pageErrorDetailResolution = iota
	pageErrorDetailResolved
)

type pageErrorDetailObservation struct {
	Resolution pageErrorDetailResolution
	Status     int
	Code       sharederrors.ErrorCode
	MessageID  sharederrors.MessageID
}

func pageErrorDetailObservationFor(err error) pageErrorDetailObservation {
	detail, status, matched := PageErrorDetailForError(err)
	if !matched {
		return pageErrorDetailObservation{Resolution: pageErrorDetailIgnored}
	}
	return pageErrorDetailObservation{
		Resolution: pageErrorDetailResolved,
		Status:     status,
		Code:       detail.Code,
		MessageID:  detail.MessageID,
	}
}

func HavePageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, ContainPageValidationFieldError(field, code, messageID))
}

func HavePageValidationField(field testmatchers.ValidationField) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, ContainPageValidationField(field))
}

func HavePageValidationFields(fields ...testmatchers.ValidationField) types.GomegaMatcher {
	matchers := make([]types.GomegaMatcher, 0, len(fields))
	for _, field := range fields {
		matchers = append(matchers, HavePageValidationField(field))
	}
	return SatisfyAll(matchers...)
}

func ContainPageValidationFieldError(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return testmatchers.ContainFieldError(field, code, messageID)
}

func ContainPageValidationField(field testmatchers.ValidationField) types.GomegaMatcher {
	fieldName := field.String()
	return WithTransform(func(validation *sharederrors.ValidationErrors) []*sharederrors.FieldError {
		if validation == nil {
			return nil
		}
		return validation.Errors
	}, ContainElement(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Field": Equal(fieldName),
	}))))
}

func respondWithPageErrorRecorder(err error) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	respondWithPageError(c, err)
	c.Writer.WriteHeaderNow()
	return rec
}

func HavePageErrorResponse(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

type pageValidationHTTPResponse struct {
	Status int
	Fields []sharederrors.FieldError
}

func HavePageValidationErrorResponse(status int, field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(pageValidationHTTPResponseFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Status": Equal(status),
		"Fields": HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Field":     Equal(field.String()),
			"Code":      Equal(code),
			"MessageID": Equal(messageID),
		})),
	}))
}

func pageValidationHTTPResponseFor(rec *httptest.ResponseRecorder) pageValidationHTTPResponse {
	if rec == nil || rec.Body == nil {
		return pageValidationHTTPResponse{}
	}
	var body struct {
		Fields []sharederrors.FieldError `json:"fields"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return pageValidationHTTPResponse{Status: rec.Code}
	}
	return pageValidationHTTPResponse{Status: rec.Code, Fields: body.Fields}
}

func HaveSectionEditErrorCode(code SectionEditErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(sectionEditErrorCodeFor, Equal(code))
}

func sectionEditErrorCodeFor(err error) SectionEditErrorCode {
	var sectionErr sectionEditError
	if !errors.As(err, &sectionErr) {
		return ""
	}
	return sectionErr.Code()
}
