package markdown

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func markdownTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-markdown-test-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func writeMarkdownTestFile(base string, rel string, content string) string {
	ginkgo.GinkgoHelper()
	path := filepath.Join(base, filepath.FromSlash(rel))
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
	return path
}

type parsedPageDocument struct {
	Document PageDocument
	Result   PageDocumentParseResult
}

func parsedPageDocumentFor(doc PageDocument, result PageDocumentParseResult) parsedPageDocument {
	return parsedPageDocument{
		Document: doc,
		Result:   result,
	}
}

type parsedPageDocumentWritebackMatcher struct {
	requiresWriteback bool
	document          types.GomegaMatcher
	description       string
}

func matchParsedDocumentWithoutWriteback(document types.GomegaMatcher) types.GomegaMatcher {
	return parsedPageDocumentWritebackMatcher{
		requiresWriteback: false,
		document:          document,
		description:       "parse canonical markdown without requesting writeback",
	}
}

func matchParsedDocumentRequiringWriteback(document types.GomegaMatcher) types.GomegaMatcher {
	return parsedPageDocumentWritebackMatcher{
		requiresWriteback: true,
		document:          document,
		description:       "parse markdown and request canonical writeback",
	}
}

func (m parsedPageDocumentWritebackMatcher) Match(actual interface{}) (bool, error) {
	got, ok := actual.(parsedPageDocument)
	if !ok {
		return false, fmt.Errorf("expected parsedPageDocument, got %T", actual)
	}
	if got.Result.RequiresWriteback != m.requiresWriteback {
		return false, nil
	}
	return m.document.Match(got.Document)
}

func (m parsedPageDocumentWritebackMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto %s", actual, m.description)
}

func (m parsedPageDocumentWritebackMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to %s", actual, m.description)
}

type markdownFileWritebackMatcher struct {
	requiresWriteback bool
	content           string
	metadata          types.GomegaMatcher
	description       string
}

func matchMarkdownFileWithoutWriteback(content string, metadata types.GomegaMatcher) types.GomegaMatcher {
	return markdownFileWritebackMatcher{
		requiresWriteback: false,
		content:           content,
		metadata:          metadata,
		description:       "keep markdown file content without requiring writeback",
	}
}

func matchMarkdownFileRequiringWriteback(content string, metadata types.GomegaMatcher) types.GomegaMatcher {
	return markdownFileWritebackMatcher{
		requiresWriteback: true,
		content:           content,
		metadata:          metadata,
		description:       "preserve markdown file content while requiring writeback",
	}
}

func (m markdownFileWritebackMatcher) Match(actual interface{}) (bool, error) {
	file, ok := actual.(*MarkdownFile)
	if !ok {
		return false, fmt.Errorf("expected *MarkdownFile, got %T", actual)
	}
	if file == nil || file.RequiresWriteback() != m.requiresWriteback || file.GetContent() != m.content {
		return false, nil
	}
	return m.metadata.Match(file.GetMetadata())
}

func (m markdownFileWritebackMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto %s", actual, m.description)
}

func (m markdownFileWritebackMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to %s", actual, m.description)
}

type markdownPathErrorMatcher struct{}

func matchMarkdownPathError() types.GomegaMatcher {
	return markdownPathErrorMatcher{}
}

func (markdownPathErrorMatcher) Match(actual interface{}) (bool, error) {
	err, ok := actual.(error)
	if !ok {
		return false, fmt.Errorf("expected error, got %T", actual)
	}
	var pathErr *os.PathError
	return errors.As(err, &pathErr), nil
}

func (markdownPathErrorMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto wrap a markdown file path error", actual)
}

func (markdownPathErrorMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to wrap a markdown file path error", actual)
}

type pageMetadataMatcher struct {
	want PageMetadata
}

func matchExactPageMetadata(want PageMetadata) types.GomegaMatcher {
	return pageMetadataMatcher{want: want}
}

func (m pageMetadataMatcher) Match(actual interface{}) (bool, error) {
	got, ok := actual.(PageMetadata)
	if !ok {
		return false, fmt.Errorf("expected PageMetadata, got %T", actual)
	}
	return Equal(m.want).Match(got)
}

func (m pageMetadataMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe canonical page metadata\n\t%#v", actual, m.want)
}

func (m pageMetadataMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe canonical page metadata\n\t%#v", actual, m.want)
}

type pageDocumentMatcher struct {
	want PageDocument
}

func matchExactPageDocument(want PageDocument) types.GomegaMatcher {
	return pageDocumentMatcher{want: want}
}

func (m pageDocumentMatcher) Match(actual interface{}) (bool, error) {
	got, ok := actual.(PageDocument)
	if !ok {
		return false, fmt.Errorf("expected PageDocument, got %T", actual)
	}
	return Equal(m.want).Match(got)
}

func (m pageDocumentMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto describe page document\n\t%#v", actual, m.want)
}

func (m pageDocumentMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to describe page document\n\t%#v", actual, m.want)
}

func exampleCanonicalMetadata() PageMetadata {
	return PageMetadata{
		Version: 1,
		Page: PageMetadataPage{
			ID:           "page-123",
			Title:        "Example Page",
			CreatedAt:    "2026-06-13T10:00:00Z",
			UpdatedAt:    "2026-06-13T11:00:00Z",
			CreatorID:    "alice",
			LastAuthorID: "bob",
		},
		Tags: []string{"research", "draft"},
		Fields: map[string]interface{}{
			"status":    "open",
			"priority":  2,
			"published": false,
		},
		Extra: map[string]interface{}{
			"aliases": []interface{}{"old-example"},
		},
	}
}
