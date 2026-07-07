package wikid

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("wikid SQLite persistence helpers", func() {
	ginkgo.Describe("sqlite helpers", func() {
		ginkgo.It("returns open errors for invalid database paths", ginkgo.Label("integration"), func() {
			_, err := openWikidDB(wikidDBPathInsideFile())
			Expect(err).To(MatchError(syscall.ENOTDIR))

			dirPath := filepath.Join(wikidTestTempDir(), "as-directory")
			Expect(os.Mkdir(dirPath, 0o755)).To(Succeed())
			_, err = openWikidDB(dirPath)
			Expect(err).To(MatchError(syscall.EISDIR))
		})

		ginkgo.It("surfaces injected file, chmod, SQL open, and initialization failures", ginkgo.Label("integration"), func() {
			restore := captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			closeErr := errors.New("close failed")
			wikidOpenFile = func(string, int, os.FileMode) (wikidCloseFile, error) {
				return closeErrorFile{err: closeErr}, nil
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(closeErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			chmodErr := errors.New("chmod failed")
			wikidChmod = func(string, os.FileMode) error {
				return chmodErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(chmodErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			sqlOpenErr := errors.New("sql open failed")
			wikidSQLOpen = func(string, string) (*sql.DB, error) {
				return nil, sqlOpenErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(sqlOpenErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			initErr := errors.New("init failed")
			wikidInitializeWikidDB = func(*sql.DB) error {
				return initErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(initErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			pragmaErr := errors.New("pragma failed")
			wikidExecSQLiteWithLockRetry = func(context.Context, wikidSQLiteExecer, string, ...any) (sql.Result, error) {
				return nil, pragmaErr
			}
			Expect(initializeWikidDB(rawSQLiteDB())).To(MatchError(pragmaErr))
		})

		ginkgo.It("rolls back immediate transactions when callbacks fail", ginkgo.Label("integration"), func() {
			path := filepath.Join(wikidTestTempDir(), "wikid.db")
			callbackErr := errors.New("callback failed")

			err := withWikidImmediateTx(path, func(context.Context, *sql.Conn) error {
				return callbackErr
			})

			Expect(err).To(MatchError(callbackErr))
		})

		ginkgo.It("surfaces connection, begin, and commit failures from immediate transactions", ginkgo.Label("integration"), func() {
			restore := captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			wikidOpenWikidDB = func(string) (*sql.DB, error) {
				db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "closed.db"))
				Expect(err).NotTo(HaveOccurred())
				Expect(db.Close()).To(Succeed())
				return db, nil
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(wikidClosedDatabaseError()))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			beginErr := errors.New("begin failed")
			wikidInitializeWikidDB = func(*sql.DB) error { return nil }
			wikidExecSQLiteWithLockRetry = func(context.Context, wikidSQLiteExecer, string, ...any) (sql.Result, error) {
				return nil, beginErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(beginErr))

			restore()
			err := withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(ctx context.Context, conn *sql.Conn) error {
				_, rollbackErr := conn.ExecContext(ctx, "ROLLBACK")
				Expect(rollbackErr).NotTo(HaveOccurred())
				return nil
			})
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			connCloseErr := errors.New("conn close failed")
			wikidCloseConn = func(*sql.Conn) error {
				return connCloseErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(connCloseErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			dbCloseErr := errors.New("db close failed")
			wikidCloseDB = func(*sql.DB) error {
				return dbCloseErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(dbCloseErr))
		})

		ginkgo.It("returns context cancellation while retrying transient SQLite lock errors", ginkgo.Label("integration"), func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			execer := &lockErrorExecer{err: wikidSQLiteErrorWithCode(5)}

			_, err := execWikidSQLiteWithLockRetry(ctx, execer, "BEGIN IMMEDIATE")

			Expect(err).To(MatchError(context.Canceled))
			Expect(execer.calls).To(Equal(1))
		})

		ginkgo.It("backs off and eventually succeeds after transient SQLite lock errors", ginkgo.Label("integration"), func() {
			execer := &lockErrorExecer{err: wikidSQLiteErrorWithCode(6), succeedAfter: 2}

			_, err := execWikidSQLiteWithLockRetry(context.Background(), execer, "BEGIN IMMEDIATE")

			Expect(err).NotTo(HaveOccurred())
			Expect(execer.calls).To(Equal(3))
		})

		ginkgo.It("parses wikid timestamps in UTC and returns parse errors", ginkgo.Label("unit"), func() {
			timestamp := time.Date(2026, 6, 27, 14, 30, 0, 123, time.FixedZone("CEST", 2*60*60))

			parsed, err := parseWikidTime(wikidTimeString(timestamp))

			Expect(err).NotTo(HaveOccurred())
			Expect(parsed.Location()).To(Equal(time.UTC))
			Expect(parsed).To(BeTemporally("==", timestamp.UTC()))
			Expect(parseWikidTime("not-a-time")).Error().To(matchWikidTimeParseError())
		})
	})
})
