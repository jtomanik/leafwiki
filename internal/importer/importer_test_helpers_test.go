package importer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var (
	errImporterRouteCandidateRejected       = errors.New("importer route candidate rejected")
	errImporterTargetKindRejected           = errors.New("importer target kind rejected")
	errImporterReferenceDestinationRejected = errors.New("importer reference destination rejected")
	errImporterSourceSuffixRejected         = errors.New("importer source suffix rejected")
	errImporterTargetSuffixRejected         = errors.New("importer target suffix rejected")
	errImporterFallbackHrefRejected         = errors.New("importer fallback href rejected")
	errImporterWikiHrefRouteRejected        = errors.New("importer wiki href route rejected")
	errImporterAssetPathRejected            = errors.New("importer asset path rejected")
	errImporterReadmeFallbackRejected       = errors.New("importer README fallback rejected")
	errImporterFrontmatterMissing           = errors.New("importer frontmatter missing")
	errImporterUpdatedContentMissing        = errors.New("importer updated content missing")
	errImporterDestinationUnchanged         = errors.New("importer destination unchanged")
	errImporterExecutionNotStarted          = errors.New("importer execution not started")
	errImporterCancellationNotRequested     = errors.New("importer cancellation not requested")
)

func importerTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-importer-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func importerWriteFile(base, rel, content string) string {
	ginkgo.GinkgoHelper()

	abs := filepath.Join(base, filepath.FromSlash(rel))
	Expect(os.MkdirAll(filepath.Dir(abs), 0o755)).To(Succeed())
	Expect(os.WriteFile(abs, []byte(content), 0o644)).To(Succeed())
	return abs
}

func importedFrontmatterResult(raw string) (markdown.Frontmatter, string, error) {
	ginkgo.GinkgoHelper()

	fm, body, has, err := markdown.ParseFrontmatter(raw)
	if err != nil {
		return markdown.Frontmatter{}, "", err
	}
	if !has {
		return markdown.Frontmatter{}, body, errImporterFrontmatterMissing
	}
	return fm, body, nil
}

func importedPageDocumentResult(raw string) (markdown.PageDocument, error) {
	ginkgo.GinkgoHelper()

	doc, _, err := markdown.ParsePageDocument(raw)
	return doc, err
}

func normalizeSourceCandidateResult(transformer *contentTransformer, candidate string) (tree.RoutePath, error) {
	ginkgo.GinkgoHelper()

	route, ok := transformer.normalizeSourceCandidateToRoutePath(candidate)
	if !ok {
		return "", errImporterRouteCandidateRejected
	}
	return newFixtureRoutePath(route), nil
}

func impliedImportTargetKindResult(href string) (tree.NodeKind, error) {
	ginkgo.GinkgoHelper()

	kind, ok := impliedImportTargetKind(href)
	if !ok {
		return "", errImporterTargetKindRejected
	}
	return kind, nil
}

func sourceSuffixLookupKeyResult(source string) (string, error) {
	ginkgo.GinkgoHelper()

	key, ok := normalizeSourcePathSuffixLookupKey(source)
	if !ok {
		return "", errImporterSourceSuffixRejected
	}
	return key, nil
}

func referenceDestinationResult(content string, lineStart int, lineEnd int) (string, string, error) {
	ginkgo.GinkgoHelper()

	label, start, end, ok := parseMarkdownReferenceDestination(content, lineStart, lineEnd)
	if !ok {
		return "", "", errImporterReferenceDestinationRejected
	}
	return label, content[start:end], nil
}

func uniqueTargetSuffixResult(transformer *contentTransformer, href string) (importTarget, error) {
	ginkgo.GinkgoHelper()

	target, ok := transformer.resolveUniqueTargetPathSuffix(href)
	if !ok {
		return importTarget{}, errImporterTargetSuffixRejected
	}
	return target, nil
}

func fallbackWikiHrefResult(transformer *contentTransformer, sourcePath tree.WorkspaceSourcePath, href string) (string, error) {
	ginkgo.GinkgoHelper()

	got, ok := transformer.fallbackWikiPageHref(sourcePath, href)
	if !ok {
		return "", errImporterFallbackHrefRejected
	}
	return got, nil
}

func wikiHrefRoutePathResult(transformer *contentTransformer, href string) (tree.RoutePath, error) {
	ginkgo.GinkgoHelper()

	got, ok := transformer.normalizeWikiHrefToRoutePath(href)
	if !ok {
		return "", errImporterWikiHrefRouteRejected
	}
	return newFixtureRoutePath(got), nil
}

func resolvedAssetPathResult(sourceBasePath string, sourcePath tree.WorkspaceSourcePath, href string) (string, error) {
	ginkgo.GinkgoHelper()

	got, ok := resolveAssetPath(sourceBasePath, sourcePath, href)
	if !ok {
		return "", errImporterAssetPathRejected
	}
	return got, nil
}

func resolvedDestinationResult(transformer *contentTransformer, userID tree.UserID, sourcePath tree.WorkspaceSourcePath, page *tree.Page, href string, wiki ImporterWiki) (string, error) {
	ginkgo.GinkgoHelper()

	got, changed, err := transformer.resolveDestination(userID, sourcePath, page, href, wiki)
	if err != nil {
		return "", err
	}
	if !changed {
		return got, errImporterDestinationUnchanged
	}
	return got, nil
}

func sourceDirReadmeFallbackResult(sourceBasePath string, sourceDir string) (string, error) {
	ginkgo.GinkgoHelper()

	got, ok := sourceDirReadmeFallbackFile(sourceBasePath, sourceDir)
	if !ok {
		return "", errImporterReadmeFallbackRejected
	}
	return got, nil
}

func updatedContentByTitleResult(contents map[string]string, title string) (string, error) {
	ginkgo.GinkgoHelper()

	content, ok := contents[title]
	if !ok {
		return "", errImporterUpdatedContentMissing
	}
	return content, nil
}

func startStoredPlanExecutionResult(store *PlanStore, userID string) (*StoredPlan, error) {
	ginkgo.GinkgoHelper()

	plan, started, err := store.TryStartExecution(userID)
	if err != nil {
		return plan, err
	}
	if !started {
		return plan, errImporterExecutionNotStarted
	}
	return plan, nil
}

func startCurrentPlanExecutionResult(service *ImporterService, userID string) (*CurrentPlanState, error) {
	ginkgo.GinkgoHelper()

	state, started, err := service.StartCurrentPlanExecution(newFixtureUserID(userID))
	if err != nil {
		return state, err
	}
	if !started {
		return state, errImporterExecutionNotStarted
	}
	return state, nil
}

func cancelCurrentPlanResult(service *ImporterService) (*CurrentPlanState, error) {
	ginkgo.GinkgoHelper()

	state, requested, err := service.CancelCurrentPlan()
	if err != nil {
		return state, err
	}
	if !requested {
		return state, errImporterCancellationNotRequested
	}
	return state, nil
}

func cleanupZipWorkspaceResult(workspace *ZipWorkspace) error {
	ginkgo.GinkgoHelper()

	return workspace.Cleanup()
}

func requestCancelResult(store *PlanStore) (*StoredPlan, error) {
	ginkgo.GinkgoHelper()

	plan, requested, err := store.RequestCancel()
	if err != nil {
		return plan, err
	}
	if !requested {
		return plan, errImporterCancellationNotRequested
	}
	return plan, nil
}

func HaveImportPlanResult(items types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Errors": BeEmpty(),
		"Items":  items,
	}))
}

func HaveImportPlanErrors(errorsMatcher types.GomegaMatcher) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Items":  BeEmpty(),
		"Errors": errorsMatcher,
	}))
}

func HaveImportedPageDocument(body types.GomegaMatcher, fields types.GomegaMatcher, extra types.GomegaMatcher) types.GomegaMatcher {
	return SatisfyAll(
		HaveField("Body", body),
		HaveField("Metadata.Fields", fields),
		HaveField("Metadata.Extra", extra),
	)
}

func HaveStoredPlanState(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

type fakeExecWikiState struct {
	EnsureCalls        int
	UpdateCalls        int
	EnsureTargets      []tree.RoutePath
	EnsureKinds        []tree.NodeKind
	UpdateTitles       []string
	LastUpdatedContent *string
	UploadCalls        int
	LastUploadByteCap  shared.MaxBytes
}

type fakeExecWikiStateMatcher struct {
	fields types.GomegaMatcher
}

func MatchFakeExecWikiState(fields types.GomegaMatcher) types.GomegaMatcher {
	return fakeExecWikiStateMatcher{fields: fields}
}

func (m fakeExecWikiStateMatcher) Match(actual any) (bool, error) {
	var state fakeExecWikiState
	switch wiki := actual.(type) {
	case *fakeExecWiki:
		state = fakeExecWikiState{
			EnsureCalls:        wiki.ensureCalls,
			UpdateCalls:        wiki.updateCalls,
			EnsureTargets:      wiki.ensureTargets,
			EnsureKinds:        wiki.ensureKinds,
			UpdateTitles:       wiki.updateTitles,
			LastUpdatedContent: wiki.lastUpdatedContent,
			UploadCalls:        wiki.uploadCalls,
			LastUploadByteCap:  wiki.lastUploadByteCap,
		}
	case *fakeWiki:
		state = fakeExecWikiState{
			EnsureCalls:        wiki.ensureCalls,
			UpdateCalls:        wiki.updateCalls,
			LastUpdatedContent: wiki.lastUpdatedContent,
		}
	default:
		return false, fmt.Errorf("expected importer fake wiki, got %T", actual)
	}
	return m.fields.Match(state)
}

func (m fakeExecWikiStateMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nto match fake wiki state", actual)
}

func (m fakeExecWikiStateMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to match fake wiki state", actual)
}

type imageOnlyReferenceMatcher struct {
	label string
}

func ReceiveImageOnlyReference(label string) types.GomegaMatcher {
	return imageOnlyReferenceMatcher{label: label}
}

func (m imageOnlyReferenceMatcher) Match(actual any) (bool, error) {
	usage, ok := actual.(importerReferenceUsage)
	if !ok {
		return false, fmt.Errorf("expected importerReferenceUsage, got %T", actual)
	}
	return usage.imageOnly(m.label), nil
}

func (m imageOnlyReferenceMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nto mark reference %q as image-only", actual, m.label)
}

func (m imageOnlyReferenceMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to mark reference %q as image-only", actual, m.label)
}
