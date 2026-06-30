package pages

import (
	"errors"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.It("TestReplaceMarkdownSection_ReplacesNestedHeadingPath", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n## API\n\n### Auth\n\nold auth\n\n### Rate Limits\n\nkeep rate limits\n\n## Other\n\nkeep other\n"

	got, err := ReplaceMarkdownSection(content, []string{"Guide", "API", "Auth"}, 0, "new auth\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection returned error: %v", err)
	}

	if !strings.Contains(got, "### Auth\nnew auth\n") {
		t.Fatalf("content = %q, want Auth body replaced", got)
	}
	if !strings.Contains(got, "### Rate Limits\n\nkeep rate limits") {
		t.Fatalf("content = %q, want sibling section preserved", got)
	}
	if !strings.Contains(got, "## Other\n\nkeep other") {
		t.Fatalf("content = %q, want unrelated section preserved", got)
	}
	if strings.Contains(got, "old auth") {
		t.Fatalf("content = %q, want old Auth body removed", got)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_IgnoresHeadingsInsideFencedCode", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n```\n## API\nfake code heading\n```\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

	got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection returned error: %v", err)
	}

	if !strings.Contains(got, "```\n## API\nfake code heading\n```") {
		t.Fatalf("content = %q, want fenced heading preserved", got)
	}
	if !strings.Contains(got, "## API\nnew api") {
		t.Fatalf("content = %q, want real API section replaced", got)
	}
	if strings.Contains(got, "old api") {
		t.Fatalf("content = %q, want old API body removed", got)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_IgnoresHeadingsInsideLongerFencedCode", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n````\n```go\n## API\nfake nested code heading\n```\n````\n\n## API\n\nold api\n"

	got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection returned error: %v", err)
	}

	if !strings.Contains(got, "````\n```go\n## API\nfake nested code heading\n```\n````") {
		t.Fatalf("content = %q, want longer fenced code heading preserved", got)
	}
	if !strings.Contains(got, "## API\nnew api") {
		t.Fatalf("content = %q, want real API section replaced", got)
	}
	if strings.Contains(got, "old api") {
		t.Fatalf("content = %q, want old API body removed", got)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_IgnoresHeadingsInsideIndentedCode", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n    ## API\n    fake indented code heading\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

	got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "new api\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection returned error: %v", err)
	}

	if !strings.Contains(got, "    ## API\n    fake indented code heading") {
		t.Fatalf("content = %q, want indented code heading preserved", got)
	}
	if !strings.Contains(got, "## API\nnew api\n") {
		t.Fatalf("content = %q, want real API section replaced", got)
	}
	if strings.Contains(got, "old api") {
		t.Fatalf("content = %q, want old API body removed", got)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_RequiresOccurrenceForAmbiguousHeading", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n## Notes\n\nfirst\n\n## Notes\n\nsecond\n"

	_, err := ReplaceMarkdownSection(content, []string{"Notes"}, 0, "new notes\n")
	if err == nil || !errors.Is(err, ErrSectionAmbiguousHeading) {
		t.Fatalf("ReplaceMarkdownSection ambiguous error = %v, want ambiguous_heading", err)
	}

	got, err := ReplaceMarkdownSection(content, []string{"Notes"}, 2, "updated notes\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection occurrence returned error: %v", err)
	}
	if !strings.Contains(got, "## Notes\n\nfirst\n\n## Notes\nupdated notes") {
		t.Fatalf("content = %q, want second occurrence replaced", got)
	}
	if strings.Contains(got, "\nsecond") {
		t.Fatalf("content = %q, want old second body removed", got)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_MissingHeadingErrorsWithoutContent", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n## API\n\nold api\n"

	_, err := ReplaceMarkdownSection(content, []string{"Missing"}, 0, "new\n")
	if err == nil || !errors.Is(err, ErrSectionHeadingNotFound) {
		t.Fatalf("ReplaceMarkdownSection missing error = %v, want heading_not_found", err)
	}
})

var _ = ginkgo.It("TestReplaceMarkdownSection_ReplacesWholeSectionWhenReplacementStartsWithSameLevelHeading", func() {
	t := ginkgo.GinkgoT()
	content := "# Guide\n\n## API\n\nold api\n\n## Other\n\nkeep other\n"

	got, err := ReplaceMarkdownSection(content, []string{"API"}, 0, "## API\nnew api\n")
	if err != nil {
		t.Fatalf("ReplaceMarkdownSection returned error: %v", err)
	}

	if strings.Count(got, "## API") != 1 {
		t.Fatalf("content = %q, want one API heading", got)
	}
	if !strings.Contains(got, "## API\nnew api\n") {
		t.Fatalf("content = %q, want API section replaced", got)
	}
	if !strings.Contains(got, "## Other\n\nkeep other") {
		t.Fatalf("content = %q, want next same-level section preserved", got)
	}
})
