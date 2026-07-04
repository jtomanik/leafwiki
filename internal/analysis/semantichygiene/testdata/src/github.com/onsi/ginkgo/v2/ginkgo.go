package ginkgo

func Describe(description string, args ...any) bool { return true }

func DescribeTable(description string, body any, entries ...any) bool { return true }

func Context(description string, args ...any) bool { return true }

func When(description string, args ...any) bool { return true }

func It(description string, args ...any) bool { return true }

func FIt(description string, args ...any) bool { return true }

func PIt(description string, args ...any) bool { return true }

func XIt(description string, args ...any) bool { return true }

func BeforeEach(body func()) bool { return true }

func Entry(description string, args ...any) any { return nil }

func Label(labels ...string) any { return nil }

func By(description string) {}

func DeferCleanup(args ...any) {}

func Skip(reason string) {}

func GinkgoRecover() {}

func Focus() any { return nil }

func Pending() any { return nil }

func Serial() any { return nil }

func Ordered() any { return nil }

func SpecPriority(priority int) any { return nil }

func FlakeAttempts(attempts int) any { return nil }
