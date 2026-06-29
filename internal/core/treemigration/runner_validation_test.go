package treemigration

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"strings"
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
	want   string
}

var missingDependencyCases = []missingDependencyCase{
	{name: "nil root", mutate: func(d *Dependencies) { d.Root = nil }, want: "tree not loaded"},
	{name: "nil store", mutate: func(d *Dependencies) { d.Store = nil }, want: "migration store is required"},
	{name: "nil log", mutate: func(d *Dependencies) { d.Log = nil }, want: "migration logger is required"},
	{name: "nil save tree", mutate: func(d *Dependencies) { d.SaveTree = nil }, want: "save tree callback is required"},
	{name: "nil save schema", mutate: func(d *Dependencies) { d.SaveSchema = nil }, want: "save schema callback is required"},
}

var _ = ginkgo.Describe("runner validation", func() {
	ginkgo.It("TestRun_RejectsNegativeFromVersion", func() {
		t := ginkgo.GinkgoT()
		deps := validDependencies()

		err := Run(-1, deps)
		if err == nil {
			t.Fatalf("expected error for negative schema version")
		}
		if !strings.Contains(err.Error(), "invalid schema version") {
			t.Fatalf("expected invalid schema version error, got: %v", err)
		}
	})

	ginkgo.Describe("TestRun_RejectsMissingRequiredDependencies", func() {
		for _, tt := range missingDependencyCases {
			tt := tt
			ginkgo.It(tt.name, func() {
				t := ginkgo.GinkgoT()
				deps := validDependencies()
				tt.mutate(&deps)
				err := Run(0, deps)
				if err == nil {
					t.Fatalf("expected error")
				}
				if !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("expected error containing %q, got: %v", tt.want, err)
				}
			})
		}
	})

	ginkgo.It("TestRun_RejectsUnsupportedMigrationVersion", func() {
		t := ginkgo.GinkgoT()
		deps := validDependencies()
		deps.CurrentSchemaVersion = 6

		err := Run(4, deps)
		if err == nil {
			t.Fatalf("expected error for unsupported migration version")
		}
		if !strings.Contains(err.Error(), "unsupported schema migration version: 5") {
			t.Fatalf("expected unsupported migration version error, got: %v", err)
		}
	})
})
