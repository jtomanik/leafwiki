package treemigration

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

type testNode struct{}

func (testNode) ID() string           { return "root" }
func (testNode) Title() string        { return "root" }
func (testNode) Slug() string         { return "root" }
func (testNode) Kind() NodeKind       { return NodeKindSection }
func (testNode) SetKind(NodeKind)     {}
func (testNode) Metadata() Metadata   { return Metadata{} }
func (testNode) SetMetadata(Metadata) {}
func (testNode) Children() []Node     { return nil }

type testStore struct{}

func (testStore) ResolveNode(Node) (*ResolvedNode, error) {
	return &ResolvedNode{Kind: NodeKindSection}, nil
}
func (testStore) ContentPathForRead(Node) (string, error)  { return "", nil }
func (testStore) ContentPathForWrite(Node) (string, error) { return "", nil }
func (testStore) EnsureSectionIndex(Node) (string, error)  { return "", nil }
func (testStore) ReadPageRaw(Node) (string, error)         { return "", nil }
func (testStore) SaveChildOrder(Node) error                { return nil }

type testLogger struct{}

func (testLogger) Info(string, ...any)  {}
func (testLogger) Warn(string, ...any)  {}
func (testLogger) Error(string, ...any) {}

func validDependencies() Dependencies {
	return Dependencies{
		Root:                 testNode{},
		Store:                testStore{},
		Log:                  testLogger{},
		CurrentSchemaVersion: 5,
		SaveTree:             func() error { return nil },
		SaveSchema:           func(int) error { return nil },
	}
}

type missingDependencyCase struct {
	name   string
	mutate func(*Dependencies)
	want   error
}

var missingDependencyCases = []missingDependencyCase{
	{name: "nil root", mutate: func(d *Dependencies) { d.Root = nil }, want: ErrMigrationRootRequired},
	{name: "nil store", mutate: func(d *Dependencies) { d.Store = nil }, want: ErrMigrationStoreRequired},
	{name: "nil log", mutate: func(d *Dependencies) { d.Log = nil }, want: ErrMigrationLoggerRequired},
	{name: "nil save tree", mutate: func(d *Dependencies) { d.SaveTree = nil }, want: ErrSaveTreeCallbackRequired},
	{name: "nil save schema", mutate: func(d *Dependencies) { d.SaveSchema = nil }, want: ErrSaveSchemaCallbackRequired},
}

func matchMigrationError(want error) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual error) (bool, error) {
		return errors.Is(actual, want), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} wrap migration error\n{{format .Data 1}}", want)
}

var _ = ginkgo.Describe("runner validation", func() {
	ginkgo.It("rejects negative stored schema versions", func() {
		deps := validDependencies()

		err := Run(-1, deps)
		Expect(err).To(HaveOccurred())
		Expect(err).To(matchMigrationError(ErrInvalidSchemaVersion))
	})

	ginkgo.Describe("required migration dependencies", func() {
		for _, tt := range missingDependencyCases {
			tt := tt
			ginkgo.It("rejects "+tt.name, func() {
				deps := validDependencies()
				tt.mutate(&deps)
				err := Run(0, deps)
				Expect(err).To(HaveOccurred())
				Expect(err).To(matchMigrationError(tt.want))
			})
		}
	})

	ginkgo.It("rejects unsupported migration versions", func() {
		deps := validDependencies()
		deps.CurrentSchemaVersion = 6

		err := Run(4, deps)
		Expect(err).To(HaveOccurred())
		Expect(err).To(matchMigrationError(ErrUnsupportedSchemaMigrationVersion))
	})
})
