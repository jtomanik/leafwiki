package assets

import (
	"bytes"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/test_utils"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

const testAssetMaxBytes shared.MaxBytes = 1024

var _ = Describe("asset service behavior", func() {
	It("stores uploaded page assets and returns public asset paths", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "lonely-page", ID: "a7b3"}
		createPageFile(tmp, "lonely-page", "# Lonely Page")
		service := NewAssetService(tmp, tree.NewSlugService())

		file, name := createMultipartFile("my-image.png", []byte("hello image"))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		url, err := service.SaveAssetForPage(page, file, assetName(name), testAssetMaxBytes)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).NotTo(BeEmpty())

		files, err := service.ListAssetsForPage(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(Equal([]string{"/assets/a7b3/my-image.png"}))
	})

	It("removes all stored assets for a page and lists none afterward", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "lonely-page", ID: "a7b3"}
		createPageFile(tmp, "lonely-page", "# Lonely Page")
		service := NewAssetService(tmp, tree.NewSlugService())

		file, name := createMultipartFile("my-image.png", []byte("hello image"))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		_, err := service.SaveAssetForPage(page, file, assetName(name), testAssetMaxBytes)
		Expect(err).NotTo(HaveOccurred())
		assetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		Expect(assetDir).To(BeADirectory())

		Expect(service.DeleteAllAssetsForPage(page)).To(Succeed())
		Expect(assetDir).NotTo(BeAnExistingFile())

		files, err := service.ListAssetsForPage(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("removes the page asset directory after deleting its last asset", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "delete-page", ID: "delete-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())

		file, name := createMultipartFile("only.png", []byte("hello image"))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		_, err := service.SaveAssetForPage(page, file, assetName(name), testAssetMaxBytes)
		Expect(err).NotTo(HaveOccurred())
		assetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		Expect(assetDir).To(BeADirectory())

		Expect(service.DeleteAsset(page, assetName(name))).To(Succeed())
		Expect(assetDir).NotTo(BeAnExistingFile())
	})

	It("generates distinct filenames for repeated uploads with the same name", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "collision-page"}
		service := NewAssetService(tmp, tree.NewSlugService())

		for i := 0; i < 3; i++ {
			file, name := createMultipartFile("logo.png", []byte("image"))
			DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

			_, err := service.SaveAssetForPage(page, file, assetName(name), testAssetMaxBytes)
			Expect(err).NotTo(HaveOccurred(), "upload %d failed", i)
		}

		files, err := service.ListAssetsForPage(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(3))
	})

	It("renames an asset and lists only the new public path", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "c3d4"}
		createPageFile(tmp, "rename-page", "# Rename Page")
		service := NewAssetService(tmp, tree.NewSlugService())

		file, name := createMultipartFile("old-name.png", []byte("old image"))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		_, err := service.SaveAssetForPage(page, file, assetName(name), testAssetMaxBytes)
		Expect(err).NotTo(HaveOccurred())

		newName := "new-name.png"
		newURL, err := service.RenameAsset(page, assetName(name), assetName(newName))
		Expect(err).NotTo(HaveOccurred())
		Expect(newURL).To(ContainSubstring(newName))

		files, err := service.ListAssetsForPage(page)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(Equal([]string{"/assets/c3d4/new-name.png"}))
	})

	It("returns localized not-found errors for missing assets", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "delete-page", ID: "delete-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		err := service.DeleteAsset(page, assetName("missing.png"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))
	})
})

type invalidSaveNameCase struct {
	originalName string
	wantCode     sharederrors.ErrorCode
}

var _ = DescribeTable("asset uploads reject invalid normalized filenames",
	func(tc invalidSaveNameCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "upload-page", ID: "upload-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		file := newTestMultipartFile([]byte("asset"))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		_, err := service.SaveAssetForPage(page, file, assetName(tc.originalName), testAssetMaxBytes)

		Expect(err).To(matchLocalizedAssetCode(tc.wantCode))
		assetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		entries, readErr := os.ReadDir(assetDir)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	},
	Entry("rejects empty names as missing names", invalidSaveNameCase{originalName: "", wantCode: ErrCodeAssetMissingName}),
	Entry("rejects current-directory names", invalidSaveNameCase{originalName: ".", wantCode: ErrCodeAssetInvalidName}),
	Entry("rejects parent-directory names", invalidSaveNameCase{originalName: "..", wantCode: ErrCodeAssetInvalidName}),
)

var _ = Describe("asset name validation guards", func() {
	It("rejects read filenames that escape the page asset directory", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "read-page", ID: "read-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		pageAssetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		Expect(os.MkdirAll(pageAssetDir, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(pageAssetDir, "note.txt"), []byte("asset"), 0o644)).To(Succeed())

		_, err := service.ReadAssetForPage(page, assetName("../note.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		_, err = service.ReadAssetForPage(page, assetName(`..\note.txt`))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
	})

	It("rejects delete filenames that target another page's assets", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "delete-page", ID: "delete-page-id"}
		other := &tree.PageNode{Slug: "other-page", ID: "other-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		otherAsset := writeAssetFile(service, other, "note.txt", []byte("other asset"))
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		err := service.DeleteAsset(page, siblingAssetName(other.ID, assetName("note.txt")))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		Expect(otherAsset).To(BeAnExistingFile())
	})

	It("rejects rename filenames that target another page's assets", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		other := &tree.PageNode{Slug: "other-page", ID: "other-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		pageAsset := writeAssetFile(service, page, "note.txt", []byte("page asset"))
		otherAsset := writeAssetFile(service, other, "note.txt", []byte("other asset"))

		_, err := service.RenameAsset(page, siblingAssetName(other.ID, assetName("note.txt")), assetName("renamed.txt"))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		Expect(otherAsset).To(BeAnExistingFile())
		_, err = service.RenameAsset(page, assetName("note.txt"), siblingAssetName(other.ID, assetName("renamed.txt")))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		Expect(pageAsset).To(BeAnExistingFile())
	})
})

type assetNameCase struct {
	filename tree.AssetName
}

var _ = DescribeTable("asset reads reject dot-component filenames",
	func(tc assetNameCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "read-page", ID: "read-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		_, err := service.ReadAssetForPage(page, tc.filename)

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
	},
	Entry("rejects current-directory names", assetNameCase{filename: assetName(".")}),
	Entry("rejects parent-directory names", assetNameCase{filename: assetName("..")}),
)

var _ = DescribeTable("asset deletes reject dot-component filenames",
	func(tc assetNameCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "delete-page", ID: "delete-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		pageAssetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		Expect(os.MkdirAll(pageAssetDir, 0o755)).To(Succeed())

		err := service.DeleteAsset(page, tc.filename)

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		Expect(pageAssetDir).To(BeADirectory())
	},
	Entry("rejects current-directory names", assetNameCase{filename: assetName(".")}),
	Entry("rejects parent-directory names", assetNameCase{filename: assetName("..")}),
)

type renameDotNameCase struct {
	oldFilename string
	newFilename string
}

var _ = DescribeTable("asset renames reject dot-component filenames",
	func(tc renameDotNameCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		pageAsset := writeAssetFile(service, page, "note.txt", []byte("page asset"))

		_, err := service.RenameAsset(page, assetName(tc.oldFilename), assetName(tc.newFilename))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
		Expect(pageAsset).To(BeAnExistingFile())
	},
	Entry("rejects current-directory source names", renameDotNameCase{oldFilename: ".", newFilename: "renamed.txt"}),
	Entry("rejects parent-directory source names", renameDotNameCase{oldFilename: "..", newFilename: "renamed.txt"}),
	Entry("rejects current-directory target names", renameDotNameCase{oldFilename: "note.txt", newFilename: "."}),
	Entry("rejects parent-directory target names", renameDotNameCase{oldFilename: "note.txt", newFilename: ".."}),
)

var _ = Describe("asset path and filename helpers", func() {
	It("builds disk paths from Windows-style asset roots", func() {
		Expect(strings.ReplaceAll(assetPageDiskPath(`C:\wiki\data\assets`, "a7b3"), `\`, `/`)).To(Equal(`C:/wiki/data/assets/a7b3`))
		Expect(strings.ReplaceAll(assetFileDiskPath(`C:\wiki\data\assets\a7b3`, "my-image.png"), `\`, `/`)).To(Equal(`C:/wiki/data/assets/a7b3/my-image.png`))
	})

	It("uses forward slashes for public asset paths", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())
		page := &tree.PageNode{ID: "a7b3"}

		Expect(service.buildPublicPath(page, assetName("my-image.png"))).To(Equal("/assets/a7b3/my-image.png"))
	})
})

var _ = DescribeTable("filename validation accepts safe asset names",
	func(tc assetNameCase) {
		Expect(validateFilename(tc.filename)).To(Succeed())
	},
	Entry("my-image.png", assetNameCase{filename: assetName("my-image.png")}),
	Entry("file.jpg", assetNameCase{filename: assetName("file.jpg")}),
	Entry("a", assetNameCase{filename: assetName("a")}),
	Entry("foo-bar.webp", assetNameCase{filename: assetName("foo-bar.webp")}),
)

var _ = DescribeTable("asset filename validation returns localized errors for empty, dot, and path names",
	func(tc invalidAssetOperationCase) {
		_, err := validateAssetFilename(tc.filename)
		Expect(err).To(matchLocalizedAssetCode(tc.wantCode))
	},
	Entry("empty", invalidAssetOperationCase{filename: assetName(""), wantCode: ErrCodeAssetMissingName}),
	Entry(".", invalidAssetOperationCase{filename: assetName("."), wantCode: ErrCodeAssetInvalidName}),
	Entry("..", invalidAssetOperationCase{filename: assetName(".."), wantCode: ErrCodeAssetInvalidName}),
	Entry("../etc/passwd", invalidAssetOperationCase{filename: assetName("../etc/passwd"), wantCode: ErrCodeAssetInvalidName}),
	Entry("../../users.db", invalidAssetOperationCase{filename: assetName("../../users.db"), wantCode: ErrCodeAssetInvalidName}),
	Entry("foo/bar.png", invalidAssetOperationCase{filename: assetName("foo/bar.png"), wantCode: ErrCodeAssetInvalidName}),
	Entry(`foo\bar.png`, invalidAssetOperationCase{filename: assetName(`foo\bar.png`), wantCode: ErrCodeAssetInvalidName}),
)

var _ = DescribeTable("asset deletes reject path traversal filenames",
	func(tc invalidAssetOperationCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "test-page", ID: "traversal-delete"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		err := service.DeleteAsset(page, tc.filename)

		Expect(err).To(matchLocalizedAssetCode(tc.wantCode))
	},
	Entry("../../users.db", invalidAssetOperationCase{filename: assetName("../../users.db"), wantCode: ErrCodeAssetInvalidName}),
	Entry("../other-page/secret.png", invalidAssetOperationCase{filename: assetName("../other-page/secret.png"), wantCode: ErrCodeAssetInvalidName}),
	Entry("..", invalidAssetOperationCase{filename: assetName(".."), wantCode: ErrCodeAssetInvalidName}),
	Entry(".", invalidAssetOperationCase{filename: assetName("."), wantCode: ErrCodeAssetInvalidName}),
	Entry("foo/bar.png", invalidAssetOperationCase{filename: assetName("foo/bar.png"), wantCode: ErrCodeAssetInvalidName}),
	Entry(`foo\bar.png`, invalidAssetOperationCase{filename: assetName(`foo\bar.png`), wantCode: ErrCodeAssetInvalidName}),
	Entry("empty", invalidAssetOperationCase{filename: assetName(""), wantCode: ErrCodeAssetMissingName}),
)

var _ = DescribeTable("asset renames reject path traversal source filenames",
	func(tc invalidAssetOperationCase) {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "test-page", ID: "traversal-rename"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		_, err := service.RenameAsset(page, tc.filename, assetName("new-name.png"))

		Expect(err).To(matchLocalizedAssetCode(tc.wantCode))
	},
	Entry("../../users.db", invalidAssetOperationCase{filename: assetName("../../users.db"), wantCode: ErrCodeAssetInvalidName}),
	Entry("../other-page/secret.png", invalidAssetOperationCase{filename: assetName("../other-page/secret.png"), wantCode: ErrCodeAssetInvalidName}),
	Entry("..", invalidAssetOperationCase{filename: assetName(".."), wantCode: ErrCodeAssetInvalidName}),
	Entry(".", invalidAssetOperationCase{filename: assetName("."), wantCode: ErrCodeAssetInvalidName}),
	Entry("foo/bar.png", invalidAssetOperationCase{filename: assetName("foo/bar.png"), wantCode: ErrCodeAssetInvalidName}),
	Entry(`foo\bar.png`, invalidAssetOperationCase{filename: assetName(`foo\bar.png`), wantCode: ErrCodeAssetInvalidName}),
	Entry("empty", invalidAssetOperationCase{filename: assetName(""), wantCode: ErrCodeAssetMissingName}),
)

type invalidAssetOperationCase struct {
	filename tree.AssetName
	wantCode sharederrors.ErrorCode
}

var _ = Describe("asset service storage boundary behavior", func() {
	It("copies regular files while skipping nested directories", func() {
		tmp := tempAssetDir()
		source := &tree.PageNode{Slug: "source", ID: "source-id"}
		target := &tree.PageNode{Slug: "target", ID: "target-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		sourceDir := filepath.Join(service.GetAssetsDir(), source.ID.String())
		Expect(os.MkdirAll(filepath.Join(sourceDir, "nested"), 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "one.png"), []byte("one"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sourceDir, "nested", "skip.png"), []byte("skip"), 0o644)).To(Succeed())

		Expect(service.CopyAllAssets(source, target)).To(Succeed())

		targetDir := filepath.Join(service.GetAssetsDir(), target.ID.String())
		raw, err := os.ReadFile(filepath.Join(targetDir, "one.png"))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("one"))
		Expect(filepath.Join(targetDir, "nested")).NotTo(BeAnExistingFile())
	})

	It("succeeds when the source page has no asset directory", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

		Expect(service.CopyAllAssets(&tree.PageNode{ID: "missing"}, &tree.PageNode{ID: "target"})).To(Succeed())
	})

	It("returns saved bytes for an existing asset", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "read-page", ID: "read-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		writeAssetFile(service, page, "note.txt", []byte("asset bytes"))

		raw, err := service.ReadAssetForPage(page, assetName("note.txt"))

		Expect(err).NotTo(HaveOccurred())
		Expect(string(raw)).To(Equal("asset bytes"))
	})

	It("returns localized not-found errors for missing assets", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "read-page", ID: "read-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		_, err := service.ReadAssetForPage(page, assetName("missing.txt"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))
	})

	It("returns localized not-found errors when the page asset directory is missing", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

		_, err := service.ReadAssetForPage(&tree.PageNode{ID: "missing-page"}, assetName("missing.txt"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))
	})

	It("rejects asset renames that change extensions", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		writeAssetFile(service, page, "note.txt", []byte("asset"))

		_, err := service.RenameAsset(page, assetName("note.txt"), assetName("note.png"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidExtension))
	})

	It("rejects asset renames with invalid slug names", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		writeAssetFile(service, page, "note.txt", []byte("asset"))

		_, err := service.RenameAsset(page, assetName("note.txt"), assetName("Bad Name.txt"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetInvalidName))
	})

	It("rejects asset renames that collide with existing targets", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		writeAssetFile(service, page, "old.txt", []byte("old"))
		writeAssetFile(service, page, "new.txt", []byte("new"))

		_, err := service.RenameAsset(page, assetName("old.txt"), assetName("new.txt"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetAlreadyExists))
	})

	It("returns localized not-found errors for missing rename sources", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "rename-page", ID: "rename-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		Expect(os.MkdirAll(filepath.Join(service.GetAssetsDir(), page.ID.String()), 0o755)).To(Succeed())

		_, err := service.RenameAsset(page, assetName("missing.txt"), assetName("new.txt"))

		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetNotFound))
	})

	It("succeeds when deleting assets for a page with no asset directory", func() {
		service := NewAssetService(tempAssetDir(), tree.NewSlugService())

		Expect(service.DeleteAllAssetsForPage(&tree.PageNode{ID: "missing-page"})).To(Succeed())
	})

	It("rejects oversized uploads without leaving partial files", func() {
		tmp := tempAssetDir()
		page := &tree.PageNode{Slug: "limit-page", ID: "limit-page-id"}
		service := NewAssetService(tmp, tree.NewSlugService())
		file, name := createMultipartFile("too-large.bin", []byte(strings.Repeat("a", 32)))
		DeferCleanup(func() { Expect(file.Close()).To(Succeed()) })

		_, err := service.SaveAssetForPage(page, file, assetName(name), 8)

		Expect(err).To(MatchError(shared.ErrFileTooLarge))
		Expect(err).To(matchLocalizedAssetCode(ErrCodeAssetFileTooLarge))
		assetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
		entries, readErr := os.ReadDir(assetDir)
		Expect(readErr).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})
})

func createPageFile(root, slug, content string) {
	GinkgoHelper()
	pagePath := filepath.Join(root, slug)
	Expect(os.MkdirAll(pagePath, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(pagePath, "index.md"), []byte(content), 0o644)).To(Succeed())
}

func tempAssetDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-assets-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func createMultipartFile(name string, content []byte) (multipart.File, string) {
	GinkgoHelper()
	file, filename, err := test_utils.CreateMultipartFile(name, content)
	Expect(err).NotTo(HaveOccurred())
	return file, filename
}

func writeAssetFile(service *AssetService, page *tree.PageNode, filename string, content []byte) string {
	GinkgoHelper()
	assetDir := filepath.Join(service.GetAssetsDir(), page.ID.String())
	Expect(os.MkdirAll(assetDir, 0o755)).To(Succeed())
	assetPath := filepath.Join(assetDir, filename)
	Expect(os.WriteFile(assetPath, content, 0o644)).To(Succeed())
	return assetPath
}

func matchLocalizedAssetCode(want sharederrors.ErrorCode) types.GomegaMatcher {
	GinkgoHelper()
	return testmatchers.MatchLocalizedError(want, sharederrors.MessageIDForCode(want))
}

type testMultipartFile struct {
	*bytes.Reader
}

func newTestMultipartFile(content []byte) multipart.File {
	return &testMultipartFile{Reader: bytes.NewReader(content)}
}

func assetName(raw string) tree.AssetName {
	return tree.AssetNameFromString(raw)
}

func siblingAssetName(pageID tree.PageID, filename tree.AssetName) tree.AssetName {
	return tree.AssetNameFromString("../" + pageID.MetadataValue() + "/" + filename.Filename())
}

func (f *testMultipartFile) Close() error {
	return nil
}
