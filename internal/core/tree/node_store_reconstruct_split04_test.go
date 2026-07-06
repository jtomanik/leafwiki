package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("node store filesystem reconstruction", ginkgo.Label("unit"), func() {
	ginkgo.It("invalid metadata timestamp falls back to mtime", func() {
		tmp := tempTreeDir()
		store := NewNodeStore(tmp)

		pagePath := filepath.Join(tmp, "root", "page.md")
		writeTreeFile(pagePath, `---
leafwiki_id: page-1
leafwiki_title: Page One
leafwiki_created_at: not-a-timestamp
leafwiki_updated_at: 2026-03-21T11:16:31Z
leafwiki_creator_id: alice
leafwiki_last_author_id: bob
---
# Page One`, 0o644)

		wantTime := time.Date(2026, time.March, 21, 12, 34, 56, 0, time.UTC)
		{
			err := os.Chtimes(pagePath, wantTime, wantTime)
			Expect(err).To(Succeed(), "Chtimes: %v",

				err)
		}

		tree, err := store.ReconstructTreeFromFS()
		Expect(err).To(Succeed(), "ReconstructTreeFromFS: %v",

			err)

		page := findChildBySlug(tree, newFixtureSlug("page"))
		{
			got := page.Metadata.CreatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal(wantTime.
				Format(time.RFC3339)),
				"expected invalid created_at to fall back to mtime, got %q",

				got)
		}
		{

			got := page.Metadata.UpdatedAt.UTC().Format(time.RFC3339)
			Expect(got).To(Equal("2026-03-21T11:16:31Z"), "expected valid updated_at to be preserved, got %q",

				got)
		}
		Expect(page.Metadata).To(SatisfyAll(
			HaveField("CreatorID", Equal(newFixtureUserID("alice"))),
			HaveField("LastAuthorID", Equal(newFixtureUserID("bob"))),
		))

	})
})
