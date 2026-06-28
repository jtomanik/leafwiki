package sqliteutil

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSQLiteUtilSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SQLite Util Suite")
}
