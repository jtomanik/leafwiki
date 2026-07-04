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

var _ = DescribeTable("SQLite recoverable error classification",
	func(tc sqliteErrorCase) {
		Expect(IsSQLiteRecoverableError(tc.err)).To(Equal(tc.want))
	},
	Entry("accepts primary disk I/O failures for database rebuild", sqliteErrorCase{err: sqliteErrorWithCode(10), want: true}),
	Entry("accepts corrupt database images for database rebuild", sqliteErrorCase{err: sqliteErrorWithCode(11), want: true}),
	Entry("accepts non-database files for database rebuild", sqliteErrorCase{err: sqliteErrorWithCode(26), want: true}),
	Entry("rejects extended out-of-memory I/O failures from rebuild recovery", sqliteErrorCase{err: sqliteErrorWithCode(10 | (12 << 8)), want: false}),
	Entry("rejects busy lock contention from rebuild recovery", sqliteErrorCase{err: sqliteErrorWithCode(5), want: false}),
	Entry("rejects locked database contention from rebuild recovery", sqliteErrorCase{err: sqliteErrorWithCode(6), want: false}),
	Entry("rejects ordinary errors from rebuild recovery", sqliteErrorCase{err: errors.New("boom"), want: false}),
)

var _ = DescribeTable("SQLite transient lock error classification",
	func(tc sqliteErrorCase) {
		Expect(IsSQLiteTransientLockError(tc.err)).To(Equal(tc.want))
	},
	Entry("recognizes busy database responses as transient lock contention", sqliteErrorCase{err: sqliteErrorWithCode(5), want: true}),
	Entry("recognizes locked database responses as transient lock contention", sqliteErrorCase{err: sqliteErrorWithCode(6), want: true}),
	Entry("rejects disk I/O failures from transient lock contention", sqliteErrorCase{err: sqliteErrorWithCode(10), want: false}),
	Entry("rejects ordinary errors from transient lock contention", sqliteErrorCase{err: errors.New("boom"), want: false}),
)

var _ = Describe("SQLite file cleanup", func() {
	It("removes the database file and known sidecar files", func() {
		dir := tempSQLiteUtilDir()
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

	It("leaves missing database and sidecar paths absent", func() {
		dir := tempSQLiteUtilDir()
		dbPath := filepath.Join(dir, "missing.db")

		RemoveSQLiteFiles(dbPath)

		for _, path := range []string{dbPath, dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"} {
			_, err := os.Stat(path)
			Expect(err).To(MatchError(os.ErrNotExist), "expected %q to remain absent", path)
		}
	})

	It("keeps going when a database path cannot be removed", func() {
		dir := tempSQLiteUtilDir()
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

var _ = Describe("SQLite error classification boundaries", func() {
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

func tempSQLiteUtilDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-sqliteutil-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func sqliteErrorWithCode(code int) error {
	e := &sqlite.Error{}
	// modernc.org/sqlite does not expose a public constructor for sqlite.Error,
	// so tests set the private `code` field directly to synthesize specific
	// result codes.
	v := reflect.ValueOf(e).Elem().FieldByName("code")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(code))
	return e
}
