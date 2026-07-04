package treemigration

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	mutate func(*Dependencies)
	want   error
}

func matchMigrationError(want error) types.GomegaMatcher {
	return WithTransform(validationMigrationErrorClassFor, Equal(validationMigrationErrorClassFor(want)))
}

type validationMigrationErrorClass string

const (
	validationMigrationErrorInvalidSchemaVersion              validationMigrationErrorClass = "invalid schema version"
	validationMigrationErrorRootRequired                      validationMigrationErrorClass = "root required"
	validationMigrationErrorStoreRequired                     validationMigrationErrorClass = "store required"
	validationMigrationErrorLoggerRequired                    validationMigrationErrorClass = "logger required"
	validationMigrationErrorSaveTreeCallbackRequired          validationMigrationErrorClass = "save tree callback required"
	validationMigrationErrorSaveSchemaCallbackRequired        validationMigrationErrorClass = "save schema callback required"
	validationMigrationErrorUnsupportedSchemaMigrationVersion validationMigrationErrorClass = "unsupported schema migration version"
	validationMigrationErrorOther                             validationMigrationErrorClass = "other migration validation error"
)

func validationMigrationErrorClassFor(err error) validationMigrationErrorClass {
	switch {
	case errors.Is(err, ErrInvalidSchemaVersion):
		return validationMigrationErrorInvalidSchemaVersion
	case errors.Is(err, ErrMigrationRootRequired):
		return validationMigrationErrorRootRequired
	case errors.Is(err, ErrMigrationStoreRequired):
		return validationMigrationErrorStoreRequired
	case errors.Is(err, ErrMigrationLoggerRequired):
		return validationMigrationErrorLoggerRequired
	case errors.Is(err, ErrSaveTreeCallbackRequired):
		return validationMigrationErrorSaveTreeCallbackRequired
	case errors.Is(err, ErrSaveSchemaCallbackRequired):
		return validationMigrationErrorSaveSchemaCallbackRequired
	case errors.Is(err, ErrUnsupportedSchemaMigrationVersion):
		return validationMigrationErrorUnsupportedSchemaMigrationVersion
	default:
		return validationMigrationErrorOther
	}
}

var _ = ginkgo.Describe("runner validation", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects negative stored schema versions", func() {
		deps := validDependencies()

		err := Run(-1, deps)
		Expect(err).To(matchMigrationError(ErrInvalidSchemaVersion))
	})

	ginkgo.DescribeTable("required migration dependencies",
		func(tc missingDependencyCase) {
			deps := validDependencies()
			tc.mutate(&deps)
			err := Run(0, deps)
			Expect(err).To(matchMigrationError(tc.want))
		},
		ginkgo.Entry("rejects migrations without a root node", missingDependencyCase{
			mutate: func(d *Dependencies) { d.Root = nil },
			want:   ErrMigrationRootRequired,
		}),
		ginkgo.Entry("rejects migrations without a storage adapter", missingDependencyCase{
			mutate: func(d *Dependencies) { d.Store = nil },
			want:   ErrMigrationStoreRequired,
		}),
		ginkgo.Entry("rejects migrations without a logger", missingDependencyCase{
			mutate: func(d *Dependencies) { d.Log = nil },
			want:   ErrMigrationLoggerRequired,
		}),
		ginkgo.Entry("rejects migrations without a save-tree callback", missingDependencyCase{
			mutate: func(d *Dependencies) { d.SaveTree = nil },
			want:   ErrSaveTreeCallbackRequired,
		}),
		ginkgo.Entry("rejects migrations without a save-schema callback", missingDependencyCase{
			mutate: func(d *Dependencies) { d.SaveSchema = nil },
			want:   ErrSaveSchemaCallbackRequired,
		}),
	)

	ginkgo.It("rejects unsupported migration versions", func() {
		deps := validDependencies()
		deps.CurrentSchemaVersion = 6

		err := Run(4, deps)
		Expect(err).To(matchMigrationError(ErrUnsupportedSchemaMigrationVersion))
	})
})
