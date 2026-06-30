package sqliteutil

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"unsafe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sqlite "modernc.org/sqlite"
)

type sqliteErrorCase struct {
	err  error
	want bool
}

var _ = DescribeTable("TestIsSQLiteRecoverableError",
	func(tc sqliteErrorCase) {
		Expect(IsSQLiteRecoverableError(tc.err)).To(Equal(tc.want))
	},
	Entry("SQLITE_IOERR", sqliteErrorCase{err: sqliteErrorWithCode(10), want: true}),
	Entry("SQLITE_CORRUPT", sqliteErrorCase{err: sqliteErrorWithCode(11), want: true}),
	Entry("SQLITE_NOTADB", sqliteErrorCase{err: sqliteErrorWithCode(26), want: true}),
	Entry("SQLITE_IOERR_NOMEM", sqliteErrorCase{err: sqliteErrorWithCode(10 | (12 << 8)), want: false}),
	Entry("SQLITE_BUSY", sqliteErrorCase{err: sqliteErrorWithCode(5), want: false}),
	Entry("SQLITE_LOCKED", sqliteErrorCase{err: sqliteErrorWithCode(6), want: false}),
	Entry("non-sqlite error", sqliteErrorCase{err: errors.New("boom"), want: false}),
)

var _ = DescribeTable("TestIsSQLiteTransientLockError",
	func(tc sqliteErrorCase) {
		Expect(IsSQLiteTransientLockError(tc.err)).To(Equal(tc.want))
	},
	Entry("SQLITE_BUSY", sqliteErrorCase{err: sqliteErrorWithCode(5), want: true}),
	Entry("SQLITE_LOCKED", sqliteErrorCase{err: sqliteErrorWithCode(6), want: true}),
	Entry("SQLITE_IOERR", sqliteErrorCase{err: sqliteErrorWithCode(10), want: false}),
	Entry("non-sqlite error", sqliteErrorCase{err: errors.New("boom"), want: false}),
)

var _ = Describe("SQLite file cleanup", func() {
	It("TestRemoveSQLiteFiles", func() {
		dir := GinkgoT().TempDir()
		dbPath := filepath.Join(dir, "search.db")
		paths := []string{dbPath, dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"}

		for _, path := range paths {
			Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
		}

		RemoveSQLiteFiles(dbPath)

		for _, path := range paths {
			_, err := os.Stat(path)
			Expect(err).To(MatchError(os.ErrNotExist), "expected %q to be removed", path)
		}
	})

	It("TestRemoveSQLiteFiles_NoOpWhenMissing", func() {
		dir := GinkgoT().TempDir()
		dbPath := filepath.Join(dir, "missing.db")

		RemoveSQLiteFiles(dbPath)

		for _, path := range []string{dbPath, dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"} {
			_, err := os.Stat(path)
			Expect(err).To(MatchError(os.ErrNotExist), "expected %q to remain absent", path)
		}
	})

	It("keeps going when a database path cannot be removed", func() {
		dir := GinkgoT().TempDir()
		dbPath := filepath.Join(dir, "search.db")
		Expect(os.Mkdir(dbPath, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dbPath, "child"), []byte("x"), 0o644)).To(Succeed())
		Expect(os.WriteFile(dbPath+"-wal", []byte("wal"), 0o644)).To(Succeed())

		RemoveSQLiteFiles(dbPath)

		info, err := os.Stat(dbPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.IsDir()).To(BeTrue())
		_, err = os.Stat(dbPath + "-wal")
		Expect(err).To(MatchError(os.ErrNotExist), "expected removable sidecar to be deleted")
	})
})

var _ = Describe("SQLite error classification edge coverage", func() {
	It("returns false for nil errors", func() {
		Expect(IsSQLiteRecoverableError(nil)).To(BeFalse())
		Expect(IsSQLiteTransientLockError(nil)).To(BeFalse())
	})

	It("treats extended recoverable result codes by their low primary byte", func() {
		Expect(IsSQLiteRecoverableError(sqliteErrorWithCode(11 | (1 << 8)))).To(BeTrue())
		Expect(IsSQLiteRecoverableError(sqliteErrorWithCode(26 | (1 << 8)))).To(BeTrue())
	})

	It("treats extended transient lock result codes by their low primary byte", func() {
		Expect(IsSQLiteTransientLockError(sqliteErrorWithCode(5 | (1 << 8)))).To(BeTrue())
		Expect(IsSQLiteTransientLockError(sqliteErrorWithCode(6 | (1 << 8)))).To(BeTrue())
	})
})

func sqliteErrorWithCode(code int) error {
	e := &sqlite.Error{}
	// modernc.org/sqlite does not expose a public constructor for sqlite.Error,
	// so tests set the private `code` field directly to synthesize specific
	// result codes.
	v := reflect.ValueOf(e).Elem().FieldByName("code")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(code))
	return e
}
