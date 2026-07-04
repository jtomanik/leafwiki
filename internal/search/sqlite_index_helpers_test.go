package search

import (
	"database/sql"
	"errors"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

type indexedPageRecord struct {
	path    string
	title   string
	content string
}

func tempSearchDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-search-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)

	return dir
}

func newSQLiteIndexForSpec() *SQLiteIndex {
	ginkgo.GinkgoHelper()

	index, err := NewSQLiteIndex(tempSearchDir())
	Expect(err).NotTo(HaveOccurred())
	closeSQLiteIndex(index)

	return index
}

func closeSQLiteIndex(index *SQLiteIndex) {
	ginkgo.GinkgoHelper()
	ginkgo.DeferCleanup(func() {
		Expect(index.Close()).To(Succeed())
	})
}

func storedPageRecord(index *SQLiteIndex, pageID tree.PageID) indexedPageRecord {
	ginkgo.GinkgoHelper()

	var record indexedPageRecord
	Expect(index.withDB(func(db *sql.DB) error {
		return db.QueryRow(`SELECT path, title, content FROM pages WHERE pageID = ?`, pageID).
			Scan(&record.path, &record.title, &record.content)
	})).To(Succeed())

	return record
}

func storedPageContent(index *SQLiteIndex, pageID tree.PageID) string {
	ginkgo.GinkgoHelper()

	return storedPageRecord(index, pageID).content
}

func matchIndexedPageRecord(path string, title string, content types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return SatisfyAll(
		WithTransform(func(record indexedPageRecord) string { return record.path }, Equal(path)),
		WithTransform(func(record indexedPageRecord) string { return record.title }, Equal(title)),
		WithTransform(func(record indexedPageRecord) string { return record.content }, content),
	)
}

func matchSearchResult(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchSearchItem(fields gstruct.Fields) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchSQLitePrimaryError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return matchSQLitePrimaryCode(sqlite3.SQLITE_ERROR)
}

func matchSQLitePrimaryCode(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}
