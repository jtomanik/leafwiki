package tree

type treeTestT interface {
	Helper()
	TempDir() string
	Fatal(args ...interface{})
	Fatalf(format string, args ...interface{})
	Error(args ...interface{})
	Errorf(format string, args ...interface{})
	Skip(args ...interface{})
}
