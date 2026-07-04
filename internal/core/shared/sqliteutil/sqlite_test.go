package sqliteutil

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"unsafe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	sqlite "modernc.org/sqlite"
)

type sqliteRecoveryCase struct {
	err      error
	expected sqliteRecoveryObservation
}

type sqliteLockCase struct {
	err      error
	expected sqliteLockObservation
}

var _ = DescribeTable("SQLite recoverable error classification", Label("unit"),
	func(tc sqliteRecoveryCase) {
		Expect(tc.err).To(matchSQLiteRecoveryObservation(tc.expected))
	},
	Entry("accepts primary disk I/O failures for database rebuild", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(10),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassDiskIO, action: sqliteRecoveryRebuildDatabaseFiles},
	}),
	Entry("accepts corrupt database images for database rebuild", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(11),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassCorruptDatabase, action: sqliteRecoveryRebuildDatabaseFiles},
	}),
	Entry("accepts non-database files for database rebuild", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(26),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassNotDatabase, action: sqliteRecoveryRebuildDatabaseFiles},
	}),
	Entry("rejects extended out-of-memory I/O failures from rebuild recovery", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(10 | (12 << 8)),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassOutOfMemoryIO, action: sqliteRecoveryPreserveDatabaseFiles},
	}),
	Entry("rejects busy lock contention from rebuild recovery", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(5),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassBusyLock, action: sqliteRecoveryPreserveDatabaseFiles},
	}),
	Entry("rejects locked database contention from rebuild recovery", sqliteRecoveryCase{
		err:      sqliteErrorWithCode(6),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassLockedDatabase, action: sqliteRecoveryPreserveDatabaseFiles},
	}),
	Entry("rejects ordinary errors from rebuild recovery", sqliteRecoveryCase{
		err:      errors.New("boom"),
		expected: sqliteRecoveryObservation{errorClass: sqliteErrorClassOrdinary, action: sqliteRecoveryPreserveDatabaseFiles},
	}),
)

var _ = DescribeTable("SQLite transient lock error classification", Label("unit"),
	func(tc sqliteLockCase) {
		Expect(tc.err).To(matchSQLiteLockObservation(tc.expected))
	},
	Entry("recognizes busy database responses as transient lock contention", sqliteLockCase{
		err:      sqliteErrorWithCode(5),
		expected: sqliteLockObservation{errorClass: sqliteErrorClassBusyLock, action: sqliteLockRetryAfterContention},
	}),
	Entry("recognizes locked database responses as transient lock contention", sqliteLockCase{
		err:      sqliteErrorWithCode(6),
		expected: sqliteLockObservation{errorClass: sqliteErrorClassLockedDatabase, action: sqliteLockRetryAfterContention},
	}),
	Entry("rejects disk I/O failures from transient lock contention", sqliteLockCase{
		err:      sqliteErrorWithCode(10),
		expected: sqliteLockObservation{errorClass: sqliteErrorClassDiskIO, action: sqliteLockNoRetry},
	}),
	Entry("rejects ordinary errors from transient lock contention", sqliteLockCase{
		err:      errors.New("boom"),
		expected: sqliteLockObservation{errorClass: sqliteErrorClassOrdinary, action: sqliteLockNoRetry},
	}),
)

var _ = Describe("SQLite file cleanup", Label("unit"), func() {
	It("removes the database file and known sidecar files", func() {
		dir := tempSQLiteUtilDir()
		dbPath := filepath.Join(dir, "search.db")
		paths := []string{dbPath, dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"}

		for _, path := range paths {
			Expect(os.WriteFile(path, []byte("x"), 0o644)).To(Succeed())
		}

		RemoveSQLiteFiles(dbPath)

		for _, path := range paths {
			Expect(path).To(matchSQLitePathState(sqlitePathAbsent), "expected %q to be removed", path)
		}
	})

	It("leaves missing database and sidecar paths absent", func() {
		dir := tempSQLiteUtilDir()
		dbPath := filepath.Join(dir, "missing.db")

		RemoveSQLiteFiles(dbPath)

		for _, path := range []string{dbPath, dbPath + "-journal", dbPath + "-wal", dbPath + "-shm"} {
			Expect(path).To(matchSQLitePathState(sqlitePathAbsent), "expected %q to remain absent", path)
		}
	})

	It("keeps going when a database path cannot be removed", func() {
		dir := tempSQLiteUtilDir()
		dbPath := filepath.Join(dir, "search.db")
		Expect(os.Mkdir(dbPath, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dbPath, "child"), []byte("x"), 0o644)).To(Succeed())
		Expect(os.WriteFile(dbPath+"-wal", []byte("wal"), 0o644)).To(Succeed())

		RemoveSQLiteFiles(dbPath)

		Expect(dbPath).To(matchSQLitePathState(sqlitePathDirectory))
		Expect(dbPath+"-wal").To(matchSQLitePathState(sqlitePathAbsent), "expected removable sidecar to be deleted")
	})
})

var _ = Describe("SQLite error classification boundaries", Label("unit"), func() {
	It("preserves database files and skips lock retries when no SQLite error is present", func() {
		Expect(nil).To(matchSQLiteRecoveryObservation(sqliteRecoveryObservation{
			errorClass: sqliteErrorClassNone,
			action:     sqliteRecoveryPreserveDatabaseFiles,
		}))
		Expect(nil).To(matchSQLiteLockObservation(sqliteLockObservation{
			errorClass: sqliteErrorClassNone,
			action:     sqliteLockNoRetry,
		}))
	})

	It("treats extended recoverable result codes by their low primary byte", func() {
		Expect(sqliteErrorWithCode(11 | (1 << 8))).To(matchSQLiteRecoveryObservation(sqliteRecoveryObservation{
			errorClass: sqliteErrorClassCorruptDatabase,
			action:     sqliteRecoveryRebuildDatabaseFiles,
		}))
		Expect(sqliteErrorWithCode(26 | (1 << 8))).To(matchSQLiteRecoveryObservation(sqliteRecoveryObservation{
			errorClass: sqliteErrorClassNotDatabase,
			action:     sqliteRecoveryRebuildDatabaseFiles,
		}))
	})

	It("treats extended transient lock result codes by their low primary byte", func() {
		Expect(sqliteErrorWithCode(5 | (1 << 8))).To(matchSQLiteLockObservation(sqliteLockObservation{
			errorClass: sqliteErrorClassBusyLock,
			action:     sqliteLockRetryAfterContention,
		}))
		Expect(sqliteErrorWithCode(6 | (1 << 8))).To(matchSQLiteLockObservation(sqliteLockObservation{
			errorClass: sqliteErrorClassLockedDatabase,
			action:     sqliteLockRetryAfterContention,
		}))
	})
})

type sqliteErrorClass string

const (
	sqliteErrorClassNone            sqliteErrorClass = "no sqlite error"
	sqliteErrorClassDiskIO          sqliteErrorClass = "disk I/O"
	sqliteErrorClassCorruptDatabase sqliteErrorClass = "corrupt database"
	sqliteErrorClassNotDatabase     sqliteErrorClass = "not database"
	sqliteErrorClassOutOfMemoryIO   sqliteErrorClass = "out-of-memory I/O"
	sqliteErrorClassBusyLock        sqliteErrorClass = "busy lock"
	sqliteErrorClassLockedDatabase  sqliteErrorClass = "locked database"
	sqliteErrorClassOrdinary        sqliteErrorClass = "ordinary error"
)

type sqliteRecoveryAction string

const (
	sqliteRecoveryRebuildDatabaseFiles  sqliteRecoveryAction = "rebuild database files"
	sqliteRecoveryPreserveDatabaseFiles sqliteRecoveryAction = "preserve database files"
)

type sqliteLockAction string

const (
	sqliteLockRetryAfterContention sqliteLockAction = "retry after contention"
	sqliteLockNoRetry              sqliteLockAction = "do not retry"
)

type sqliteRecoveryObservation struct {
	errorClass sqliteErrorClass
	action     sqliteRecoveryAction
}

type sqliteLockObservation struct {
	errorClass sqliteErrorClass
	action     sqliteLockAction
}

func matchSQLiteRecoveryObservation(expected sqliteRecoveryObservation) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(sqliteRecoveryObservationFor, Equal(expected))
}

func sqliteRecoveryObservationFor(err error) sqliteRecoveryObservation {
	action := sqliteRecoveryPreserveDatabaseFiles
	if IsSQLiteRecoverableError(err) {
		action = sqliteRecoveryRebuildDatabaseFiles
	}

	return sqliteRecoveryObservation{
		errorClass: sqliteErrorClassFor(err),
		action:     action,
	}
}

func matchSQLiteLockObservation(expected sqliteLockObservation) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(sqliteLockObservationFor, Equal(expected))
}

func sqliteLockObservationFor(err error) sqliteLockObservation {
	action := sqliteLockNoRetry
	if IsSQLiteTransientLockError(err) {
		action = sqliteLockRetryAfterContention
	}

	return sqliteLockObservation{
		errorClass: sqliteErrorClassFor(err),
		action:     action,
	}
}

func sqliteErrorClassFor(err error) sqliteErrorClass {
	if err == nil {
		return sqliteErrorClassNone
	}

	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return sqliteErrorClassOrdinary
	}

	switch sqliteErr.Code() {
	case 10 | (12 << 8):
		return sqliteErrorClassOutOfMemoryIO
	}

	switch sqliteErr.Code() & 0xFF {
	case 5:
		return sqliteErrorClassBusyLock
	case 6:
		return sqliteErrorClassLockedDatabase
	case 10:
		return sqliteErrorClassDiskIO
	case 11:
		return sqliteErrorClassCorruptDatabase
	case 26:
		return sqliteErrorClassNotDatabase
	default:
		return sqliteErrorClassOrdinary
	}
}

type sqlitePathState string

const (
	sqlitePathAbsent    sqlitePathState = "absent"
	sqlitePathDirectory sqlitePathState = "directory"
	sqlitePathFile      sqlitePathState = "file"
	sqlitePathOther     sqlitePathState = "other filesystem state"
)

func matchSQLitePathState(expected sqlitePathState) types.GomegaMatcher {
	GinkgoHelper()

	return WithTransform(sqlitePathStateFor, Equal(expected))
}

func sqlitePathStateFor(path string) sqlitePathState {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return sqlitePathAbsent
	}
	if err != nil {
		return sqlitePathOther
	}
	if info.IsDir() {
		return sqlitePathDirectory
	}
	return sqlitePathFile
}

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
