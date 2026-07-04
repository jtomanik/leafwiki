package workspacesync

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("workspace sync path normalization", Label("unit"), func() {
	It("cleans and joins markdown paths without losing section filenames", func() {
		Expect(cleanWorkspaceMarkdownPath(" /docs/section/index.md ")).To(Equal("docs/section/index.md"))
		Expect(cleanWorkspaceMarkdownPath("./docs/page.md")).To(Equal("./docs/page.md"))
		Expect(joinWorkspaceMarkdownPath("", "/docs/", " section ", "index.md")).To(Equal("docs/section/index.md"))
		Expect(joinWorkspaceMarkdownPath("docs", "", "/page.md/")).To(Equal("docs/page.md"))
	})

	It("selects section index content before README fallback", func() {
		rootDir := workspaceSyncTempDir()
		service := &Service{rootDir: rootDir}
		Expect(os.MkdirAll(filepath.Join(rootDir, "docs"), 0o755)).To(Succeed())
		writeMarkdownFile(filepath.Join(rootDir, "docs", "README.md"), "# Readme\n")

		Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/README.md"))

		writeMarkdownFile(filepath.Join(rootDir, "docs", "Index.MD"), "# Index\n")
		Expect(service.currentSectionContentPath("docs", "docs.md")).To(Equal("docs/Index.MD"))
		Expect(service.currentSectionContentPath("missing", "fallback.md")).To(Equal("fallback.md"))
	})

	It("keeps validation paths workspace-relative unless they escape the root", func() {
		rootDir := filepath.Join(workspaceSyncTempDir(), "workspace")
		Expect(normalizeValidationPath(rootDir, filepath.Join(rootDir, "docs", "page.md"))).To(Equal("docs/page.md"))
		Expect(normalizeValidationPath(rootDir, "./docs/page.md")).To(Equal("docs/page.md"))
		Expect(normalizeValidationPath(rootDir, "../outside.md")).To(BeEmpty())
		Expect(normalizeValidationPath(rootDir, filepath.Join(filepath.Dir(rootDir), "outside.md"))).To(Equal(filepath.ToSlash(filepath.Join(filepath.Dir(rootDir), "outside.md"))))
	})

	It("returns the first trimmed non-blank value", func() {
		Expect(firstNonEmpty("", " \t ", " value ", "later")).To(Equal("value"))
		Expect(firstNonEmpty("", " ")).To(BeEmpty())
	})
})
