package treemigration

import (
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

type migrationNodeID string
type migrationNodeSlug string

const legacyMigrationNodeKind NodeKind = "legacy"

func migrationNodeIDFromString(raw string) migrationNodeID {
	return migrationNodeID(raw)
}

func (id migrationNodeID) String() string {
	return string(id)
}

func (slug migrationNodeSlug) String() string {
	return string(slug)
}

type mutableMigrationNode struct {
	id       migrationNodeID
	title    string
	slug     migrationNodeSlug
	kind     NodeKind
	metadata Metadata
	children []Node
}

func (n *mutableMigrationNode) ID() string                { return n.id.String() }
func (n *mutableMigrationNode) Title() string             { return n.title }
func (n *mutableMigrationNode) Slug() string              { return n.slug.String() }
func (n *mutableMigrationNode) Kind() NodeKind            { return n.kind }
func (n *mutableMigrationNode) SetKind(kind NodeKind)     { n.kind = kind }
func (n *mutableMigrationNode) Metadata() Metadata        { return n.metadata }
func (n *mutableMigrationNode) SetMetadata(meta Metadata) { n.metadata = meta }
func (n *mutableMigrationNode) Children() []Node          { return n.children }

type configurableMigrationStore struct {
	resolved       *ResolvedNode
	resolvedByID   map[migrationNodeID]*ResolvedNode
	resolveErr     error
	resolveErrByID map[migrationNodeID]error
	resolveN       int

	readPath        string
	readPathByID    map[migrationNodeID]string
	readPathErr     error
	readPathErrByID map[migrationNodeID]error

	writePath        string
	writePathByID    map[migrationNodeID]string
	writePathErr     error
	writePathErrByID map[migrationNodeID]error

	readRaw        string
	readRawByID    map[migrationNodeID]string
	readErr        error
	readErrByID    map[migrationNodeID]error
	saveErrByID    map[migrationNodeID]error
	savedOrderIDs  []migrationNodeID
	ensureErrByID  map[migrationNodeID]error
	ensuredIndexID []migrationNodeID
}

func lookupMigrationNodeID(node Node) migrationNodeID {
	if node == nil {
		return ""
	}
	return migrationNodeIDFromString(node.ID())
}

func (s *configurableMigrationStore) ResolveNode(node Node) (*ResolvedNode, error) {
	s.resolveN++
	id := lookupMigrationNodeID(node)
	if err, ok := s.resolveErrByID[id]; ok {
		return nil, err
	}
	if s.resolveErr != nil {
		return nil, s.resolveErr
	}
	if resolved, ok := s.resolvedByID[id]; ok {
		return resolved, nil
	}
	return s.resolved, nil
}

func (s *configurableMigrationStore) ContentPathForRead(node Node) (string, error) {
	id := lookupMigrationNodeID(node)
	if err, ok := s.readPathErrByID[id]; ok {
		return "", err
	}
	if s.readPathErr != nil {
		return "", s.readPathErr
	}
	if path, ok := s.readPathByID[id]; ok {
		return path, nil
	}
	return s.readPath, nil
}

func (s *configurableMigrationStore) ContentPathForWrite(node Node) (string, error) {
	id := lookupMigrationNodeID(node)
	if err, ok := s.writePathErrByID[id]; ok {
		return "", err
	}
	if s.writePathErr != nil {
		return "", s.writePathErr
	}
	if path, ok := s.writePathByID[id]; ok {
		return path, nil
	}
	return s.writePath, nil
}

func (s *configurableMigrationStore) EnsureSectionIndex(node Node) (string, error) {
	id := lookupMigrationNodeID(node)
	if err, ok := s.ensureErrByID[id]; ok {
		return "", err
	}
	s.ensuredIndexID = append(s.ensuredIndexID, id)
	return "", nil
}

func (s *configurableMigrationStore) ReadPageRaw(node Node) (string, error) {
	id := lookupMigrationNodeID(node)
	if err, ok := s.readErrByID[id]; ok {
		return "", err
	}
	if s.readErr != nil {
		return "", s.readErr
	}
	if raw, ok := s.readRawByID[id]; ok {
		return raw, nil
	}
	return s.readRaw, nil
}

func (s *configurableMigrationStore) SaveChildOrder(node Node) error {
	id := lookupMigrationNodeID(node)
	s.savedOrderIDs = append(s.savedOrderIDs, id)
	if err, ok := s.saveErrByID[id]; ok {
		return err
	}
	return nil
}

type recordingMigrationLogger struct {
	errors   []string
	infos    []string
	warnings []string
}

func (l *recordingMigrationLogger) Info(msg string, _ ...any) {
	l.infos = append(l.infos, msg)
}
func (l *recordingMigrationLogger) Warn(msg string, _ ...any) {
	l.warnings = append(l.warnings, msg)
}
func (l *recordingMigrationLogger) Error(msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}

type expectedMigrationMessages struct {
	Warnings []string
	Infos    []string
}

func recordMigrationMessages(expected expectedMigrationMessages) OmegaMatcher {
	return WithTransform(recordedMigrationMessagesFor, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Warnings": ContainElements(expected.Warnings),
		"Infos":    ContainElements(expected.Infos),
	}))
}

type recordedMigrationMessages struct {
	Warnings []string
	Infos    []string
}

func recordedMigrationMessagesFor(log *recordingMigrationLogger) recordedMigrationMessages {
	if log == nil {
		return recordedMigrationMessages{}
	}
	return recordedMigrationMessages{
		Warnings: log.warnings,
		Infos:    log.infos,
	}
}

type migrationMetadataTimestampState string

const (
	migrationMetadataTimestampsMissing    migrationMetadataTimestampState = "missing metadata timestamps"
	migrationMetadataTimestampsBackfilled migrationMetadataTimestampState = "backfilled metadata timestamps"
	migrationMetadataTimestampsPartial    migrationMetadataTimestampState = "partial metadata timestamps"
)

func matchMigrationMetadataTimestampState(expected migrationMetadataTimestampState) OmegaMatcher {
	return WithTransform(migrationMetadataTimestampStateFor, Equal(expected))
}

func migrationMetadataTimestampStateFor(meta Metadata) migrationMetadataTimestampState {
	createdMissing := meta.CreatedAt.IsZero()
	updatedMissing := meta.UpdatedAt.IsZero()
	switch {
	case createdMissing && updatedMissing:
		return migrationMetadataTimestampsMissing
	case !createdMissing && !updatedMissing:
		return migrationMetadataTimestampsBackfilled
	default:
		return migrationMetadataTimestampsPartial
	}
}

func tempMigrationScratchDir() string {
	ginkgo.GinkgoHelper()

	path, err := os.MkdirTemp("", "leafwiki-treemigration-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, path)
	return path
}
