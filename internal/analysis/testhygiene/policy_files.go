package testhygiene

import "strings"

func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go")
}

func isTestSupportFile(filename string) bool {
	return strings.Contains(filename, "/internal/test_utils/")
}
