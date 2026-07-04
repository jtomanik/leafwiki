package importer

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"syscall"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("ZipExtractor safety edges", ginkgo.Label("unit"), func() {
	ginkgo.It("skips empty and directory entries while extracting regular files", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		_, err := zipWriter.Create("   ")
		Expect(err).NotTo(HaveOccurred())
		_, err = zipWriter.Create("docs/")
		Expect(err).NotTo(HaveOccurred())
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(workspace.Cleanup)
		Expect(os.ReadFile(filepath.Join(workspace.Root, "docs", "page.md"))).To(Equal([]byte("# Page")))
	})

	ginkgo.It("reports base directory creation errors before extracting", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())
		baseDir := filepath.Join(importerTempDir(), "not-a-dir")
		Expect(os.WriteFile(baseDir, []byte("file"), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.ENOTDIR))
	})

	ginkgo.It("reports temp workspace creation errors", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "safe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())
		baseDir := filepath.Join(importerTempDir(), "readonly")
		Expect(os.MkdirAll(baseDir, 0o755)).To(Succeed())
		Expect(os.Chmod(baseDir, 0o555)).To(Succeed())
		ginkgo.DeferCleanup(func() {
			_ = os.Chmod(baseDir, 0o755)
		})

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.EACCES))
	})

	ginkgo.It("reports per-entry directory and file creation failures", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("blocked")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("not a directory"))
		Expect(err).NotTo(HaveOccurred())
		file, err = zipWriter.Create("blocked/page.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "blocked-dir.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.ENOTDIR))

		zipBytes.Reset()
		zipWriter = zip.NewWriter(&zipBytes)
		file, err = zipWriter.Create("blocked/file.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		file, err = zipWriter.Create("blocked")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("not a directory"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath = filepath.Join(importerTempDir(), "blocked-file.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		workspace, err = NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(syscall.EISDIR))
	})

	ginkgo.It("reports corrupt entry content", func() {
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		header := &zip.FileHeader{Name: "docs/page.md", Method: zip.Store}
		file, err := zipWriter.CreateHeader(header)
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# Page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		unsupported := append([]byte(nil), zipBytes.Bytes()...)
		localHeader := bytes.Index(unsupported, []byte{'P', 'K', 3, 4})
		centralHeader := bytes.Index(unsupported, []byte{'P', 'K', 1, 2})
		Expect(localHeader).To(BeNumerically(">=", 0))
		Expect(centralHeader).To(BeNumerically(">=", 0))
		unsupported[localHeader+8] = 99
		unsupported[localHeader+9] = 0
		unsupported[centralHeader+10] = 99
		unsupported[centralHeader+11] = 0
		zipPath := filepath.Join(importerTempDir(), "unsupported.zip")
		Expect(os.WriteFile(zipPath, unsupported, 0o644)).To(Succeed())
		workspace, err := NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(zip.ErrAlgorithm))

		corrupt := bytes.Replace(zipBytes.Bytes(), []byte("# Page"), []byte("# Paga"), 1)
		zipPath = filepath.Join(importerTempDir(), "corrupt.zip")
		Expect(os.WriteFile(zipPath, corrupt, 0o644)).To(Succeed())

		workspace, err = NewZipExtractor().ExtractToDir(zipPath, importerTempDir())
		Expect(workspace).To(BeNil())
		Expect(err).To(MatchError(zip.ErrChecksum))
	})

	ginkgo.It("rejects unsafe zip entry paths and cleans up the failed workspace", func() {
		baseDir := importerTempDir()
		var zipBytes bytes.Buffer
		zipWriter := zip.NewWriter(&zipBytes)
		file, err := zipWriter.Create("../escape.md")
		Expect(err).NotTo(HaveOccurred())
		_, err = file.Write([]byte("# escape"))
		Expect(err).NotTo(HaveOccurred())
		Expect(zipWriter.Close()).To(Succeed())

		zipPath := filepath.Join(importerTempDir(), "unsafe.zip")
		Expect(os.WriteFile(zipPath, zipBytes.Bytes(), 0o644)).To(Succeed())

		_, err = NewZipExtractor().ExtractToDir(zipPath, baseDir)
		Expect(err).To(SatisfyAll(
			MatchError(ErrImportZipInvalidEntry),
			MatchError(ErrImportZipPathTraversal),
		))

		entries, err := os.ReadDir(baseDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
	})

	ginkgo.It("keeps safe joined paths inside the extraction root", func() {
		baseDir := filepath.Join(importerTempDir(), "root")

		joined, err := safeJoin(baseDir, "docs/page.md")
		Expect(err).NotTo(HaveOccurred())
		Expect(joined).To(Equal(filepath.Join(baseDir, "docs", "page.md")))

		_, err = safeJoin(baseDir, "../escape.md")
		Expect(err).To(MatchError(ErrImportZipPathTraversal))

		_, err = safeJoin(baseDir, "/absolute.md")
		Expect(err).To(MatchError(ErrImportZipAbsolutePath))
	})

	ginkgo.It("reports missing zip files", func() {
		_, err := NewZipExtractor().ExtractToDir(filepath.Join(importerTempDir(), "missing.zip"), importerTempDir())
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	ginkgo.It("treats nil or empty zip workspaces as already clean", func() {
		var workspace *ZipWorkspace
		Expect(cleanupZipWorkspaceResult(workspace)).To(Succeed())
		Expect(cleanupZipWorkspaceResult(&ZipWorkspace{})).To(Succeed())
	})
})
