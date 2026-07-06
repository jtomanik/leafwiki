package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("markdown link extraction", ginkgo.Label("unit"), func() {
	ginkgo.It("returns normalized wiki destinations while filtering external anchors and query details", func() {
		md := `
# Example

Internal: [Page 1](/docs/page1)
Relative: [Rel](../docs/page2)
Anchor only: [Section](#heading)
External: [Google](https://google.com)
Mail: [Mail](mailto:test@example.com)
With fragment: [WithFragment](/docs/page3#intro)
With query: [WithQuery](/docs/page4?foo=bar)
With both: [Both](/docs/page5?foo=bar#section)
`

		links := extractLinksFromMarkdown(md)

		want := []string{
			"/docs/page1",
			"../docs/page2",
			"/docs/page3",
			"/docs/page4",
			"/docs/page5",
		}

		Expect(links).To(Equal(want))
	})

	ginkgo.It("filters external schemes case-insensitively", func() {
		md := `
[HTTPS](HTTPS://example.com)
[Mail](Mailto:test@example.com)
[Anchor](#intro)
[Wiki](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})

	ginkgo.It("leaves asset destinations out of wiki link extraction", func() {
		md := `
Asset absolute: [File](/assets/abc/manual.pdf)
Asset relative: [Image](assets/abc/picture.png)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})

	ginkgo.It("ignores image links even when they point at page destinations", func() {
		md := `
Image page path: ![Alt](/docs/b.md)
Normal page link: [Page](/docs/b.md)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/b.md"}))
	})

	ginkgo.It("filters mixed-case external schemes while keeping wiki links", func() {
		md := `
Uppercase HTTPS: [A](HTTPS://example.com)
Uppercase HTTP: [B](HTTP://example.com)
Uppercase MAILTO: [C](MAILTO:foo@bar.com)
Mixed case Https: [D](Https://example.com)
Mixed case Mailto: [E](Mailto:foo@bar.com)
Internal: [Page](/docs/page1)
`

		links := extractLinksFromMarkdown(md)

		Expect(links).To(Equal([]string{"/docs/page1"}))
	})
})

var _ = ginkgo.Describe("link service refactor matches", ginkgo.Label("integration"), func() {
	ginkgo.It("accepts route paths when matching descendants by prefix and kind", func() {
		store, err := NewLinksStore(linksTempDir())
		Expect(err).NotTo(HaveOccurred())

		Expect(store.AddLinks(newFixturePageID("source"), "Source", []TargetLink{
			{TargetPageID: newFixturePageID("target"), TargetPagePath: "/docs/guide", TargetKind: TargetKindSection},
			{TargetPageID: newFixturePageID("child"), TargetPagePath: "/docs/guide/child", TargetKind: TargetKindSection},
		})).To(Succeed())

		service := NewLinkService(linksTempDir(), nil, store)
		matches, err := service.GetRefactorMatchesForPrefixAndKind(newFixtureRoutePath("docs/guide"), tree.NodeKindSection)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(HaveLen(2))
	})
})

// helper to create a small tree structure:
// root
//
//	└─ docs
//	     ├─ page1
