package treemigration

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/core/markdown"
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
	return Satisfy(func(log *recordingMigrationLogger) bool {
		if log == nil {
			return false
		}
		warningsOK, err := ContainElements(expected.Warnings).Match(log.warnings)
		if err != nil || !warningsOK {
			return false
		}
		infosOK, err := ContainElements(expected.Infos).Match(log.infos)
		return err == nil && infosOK
	})
}

var _ = ginkgo.Describe("runner helper coverage", func() {
	ginkgo.It("backfillMetadata uses filesystem modtime and preserves author metadata", func() {
		tmp := ginkgo.GinkgoT().TempDir()
		path := filepath.Join(tmp, "page.md")
		Expect(os.WriteFile(path, []byte("# Page\n"), 0o644)).To(Succeed())
		modTime := time.Date(2026, 6, 25, 10, 11, 12, 0, time.FixedZone("offset", 2*60*60))
		Expect(os.Chtimes(path, modTime, modTime)).To(Succeed())

		node := &mutableMigrationNode{
			id:    "page-1",
			title: "Page",
			slug:  "page",
			kind:  NodeKindPage,
			metadata: Metadata{
				CreatorID:    "alice",
				LastAuthorID: "bob",
			},
		}
		store := &configurableMigrationStore{resolved: &ResolvedNode{Kind: NodeKindPage, FilePath: path, HasContent: true}}
		log := &recordingMigrationLogger{}

		Expect(backfillMetadata(Dependencies{Store: store, Log: log}, node)).To(Succeed())

		meta := node.Metadata()
		Expect(meta).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"CreatedAt":    BeTemporally("==", modTime.UTC()),
			"UpdatedAt":    BeTemporally("==", modTime.UTC()),
			"CreatorID":    Equal("alice"),
			"LastAuthorID": Equal("bob"),
		}))
		Expect(log.errors).To(BeEmpty())
	})

	ginkgo.It("backfillMetadata skips nodes that already have metadata", func() {
		existing := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		node := &mutableMigrationNode{
			id:       "page-1",
			metadata: Metadata{CreatedAt: existing, UpdatedAt: existing},
		}
		store := &configurableMigrationStore{}

		Expect(backfillMetadata(Dependencies{Store: store, Log: &recordingMigrationLogger{}}, node)).To(Succeed())

		Expect(store.resolveN).To(BeZero())
		Expect(node.Metadata().CreatedAt).To(BeTemporally("==", existing))
	})

	ginkgo.It("backfillMetadata treats resolve-node errors as logged non-fatal misses", func() {
		node := &mutableMigrationNode{id: "page-1"}
		store := &configurableMigrationStore{resolveErr: errors.New("resolve failed")}
		log := &recordingMigrationLogger{}

		Expect(backfillMetadata(Dependencies{Store: store, Log: log}, node)).To(Succeed())

		Expect(node.Metadata().CreatedAt.IsZero()).To(BeTrue())
		Expect(log.errors).To(ContainElement("Could not resolve node for metadata backfill"))
	})

	ginkgo.It("backfillMetadata handles nil nodes and non-missing stat errors without aborting", func() {
		Expect(backfillMetadata(Dependencies{}, nil)).To(Succeed())

		originalStatFile := statFile
		statFile = func(string) (os.FileInfo, error) {
			return nil, errors.New("stat failed")
		}
		ginkgo.DeferCleanup(func() {
			statFile = originalStatFile
		})

		child := &mutableMigrationNode{id: "child", title: "Child", slug: "child", kind: NodeKindPage}
		node := &mutableMigrationNode{id: "page-1", title: "Page", slug: "page", kind: NodeKindPage, children: []Node{child}}
		store := &configurableMigrationStore{resolved: &ResolvedNode{Kind: NodeKindPage, FilePath: "/missing-but-not-used.md"}}
		log := &recordingMigrationLogger{}

		Expect(backfillMetadata(Dependencies{Store: store, Log: log}, node)).To(Succeed())

		Expect(node.Metadata().CreatedAt.IsZero()).To(BeFalse())
		Expect(child.Metadata().CreatedAt.IsZero()).To(BeFalse())
		Expect(log.errors).To(ContainElement("Could not stat node for metadata"))
	})

	ginkgo.It("backfillChildOrder handles nil nodes and propagates child order write failures", func() {
		Expect(backfillChildOrder(Dependencies{}, nil)).To(Succeed())

		grandchild := &mutableMigrationNode{id: "grandchild", kind: NodeKindPage}
		child := &mutableMigrationNode{id: "child", kind: NodeKindSection, children: []Node{grandchild}}
		root := &mutableMigrationNode{id: "root", kind: NodeKindSection, children: []Node{child}}
		saveFailedErr := errors.New("save failed")
		store := &configurableMigrationStore{
			saveErrByID: map[migrationNodeID]error{"child": saveFailedErr},
		}

		err := backfillChildOrder(Dependencies{Store: store, Log: &recordingMigrationLogger{}}, root)

		Expect(err).To(MatchError(saveFailedErr))
		Expect(store.savedOrderIDs).To(Equal([]migrationNodeID{"root", "child"}))
	})

	ginkgo.It("migrateToV2 logs and returns managed metadata failures for root children", func() {
		child := &mutableMigrationNode{id: "page-1", kind: NodeKindPage}
		root := &mutableMigrationNode{id: "root", kind: NodeKindSection, children: []Node{child}}
		readFailedErr := errors.New("read failed")
		store := &configurableMigrationStore{
			readErrByID: map[migrationNodeID]error{"page-1": readFailedErr},
		}
		log := &recordingMigrationLogger{}

		err := migrateToV2(Dependencies{Root: root, Store: store, Log: log})

		Expect(err).To(MatchError(readFailedErr))
		Expect(log.errors).To(ContainElements(
			"Could not read page content for node",
			"Error adding metadata to child node",
		))
	})

	ginkgo.It("backfillKindFromFS resolves unknown kinds and falls back to child-aware heuristics", func() {
		Expect(func() { backfillKindFromFS(Dependencies{}, nil) }).ToNot(Panic())

		resolved := &mutableMigrationNode{id: "resolved", kind: legacyMigrationNodeKind}
		grandchild := &mutableMigrationNode{id: "grandchild", kind: NodeKindPage}
		heuristicSection := &mutableMigrationNode{id: "section", kind: legacyMigrationNodeKind, children: []Node{grandchild}}
		heuristicPage := &mutableMigrationNode{id: "page", kind: legacyMigrationNodeKind}
		root := &mutableMigrationNode{
			id:       "root",
			kind:     legacyMigrationNodeKind,
			children: []Node{nil, resolved, heuristicSection, heuristicPage},
		}
		store := &configurableMigrationStore{
			resolvedByID: map[migrationNodeID]*ResolvedNode{
				"resolved": {Kind: NodeKindSection},
			},
			resolveErrByID: map[migrationNodeID]error{
				"section": errors.New("not found"),
				"page":    errors.New("not found"),
			},
		}
		log := &recordingMigrationLogger{}

		backfillKindFromFS(Dependencies{Store: store, Log: log}, root)

		Expect(root.Kind()).To(Equal(NodeKindSection))
		Expect(resolved.Kind()).To(Equal(NodeKindSection))
		Expect(heuristicSection.Kind()).To(Equal(NodeKindSection))
		Expect(heuristicPage.Kind()).To(Equal(NodeKindPage))
		Expect(log.warnings).To(ConsistOf(
			"could not resolve node on disk; kind backfilled by heuristic",
			"could not resolve node on disk; kind backfilled by heuristic",
		))
	})

	ginkgo.It("addManagedMetadata recurses through missing parent content", func() {
		missingContentErr := errors.New("missing content")
		childPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "child.md")
		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		store := &configurableMigrationStore{
			readErrByID:   map[migrationNodeID]error{"parent": missingContentErr},
			readRawByID:   map[migrationNodeID]string{"child": "# Child\n"},
			writePathByID: map[migrationNodeID]string{"child": childPath},
		}
		log := &recordingMigrationLogger{}

		err := addManagedMetadata(Dependencies{
			Store:               store,
			Log:                 log,
			IsMissingContentErr: func(err error) bool { return errors.Is(err, missingContentErr) },
		}, parent)

		Expect(err).ToNot(HaveOccurred())
		Expect(log).To(recordMigrationMessages(expectedMigrationMessages{
			Warnings: []string{"Page file does not exist, skipping metadata addition"},
			Infos:    []string{"metadata backfilled"},
		}))
		raw, err := os.ReadFile(childPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(raw)).To(ContainSubstring("id: child"))
	})

	ginkgo.It("addManagedMetadata logs child failures from missing-content parents", func() {
		missingContentErr := errors.New("missing content")
		childErr := errors.New("child read failed")
		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		store := &configurableMigrationStore{
			readErrByID: map[migrationNodeID]error{
				"parent": missingContentErr,
				"child":  childErr,
			},
		}
		log := &recordingMigrationLogger{}

		err := addManagedMetadata(Dependencies{
			Store:               store,
			Log:                 log,
			IsMissingContentErr: func(err error) bool { return errors.Is(err, missingContentErr) },
		}, parent)

		Expect(err).To(MatchError(childErr))
		Expect(log.errors).To(ContainElement("Error adding metadata to child node"))
	})

	ginkgo.It("addManagedMetadata reports write-path, parse, write, and recursive child failures", func() {
		tmp := ginkgo.GinkgoT().TempDir()
		page := &mutableMigrationNode{id: "page", title: "Page", kind: NodeKindPage}

		pathErr := errors.New("path failed")
		Expect(addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID:      map[migrationNodeID]string{"page": "# Page\n"},
				writePathErrByID: map[migrationNodeID]error{"page": pathErr},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(pathErr))

		Expect(addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID:   map[migrationNodeID]string{"page": "---\ninvalid: [\n---\n# Page\n"},
				writePathByID: map[migrationNodeID]string{"page": filepath.Join(tmp, "parse.md")},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(markdown.ErrFrontmatterParse))

		originalWriteMarkdownFile := writeMarkdownFile
		writeErr := errors.New("write failed")
		writeMarkdownFile = func(*markdown.MarkdownFile) error {
			return writeErr
		}
		ginkgo.DeferCleanup(func() {
			writeMarkdownFile = originalWriteMarkdownFile
		})

		Expect(addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID:   map[migrationNodeID]string{"page": "# Page\n"},
				writePathByID: map[migrationNodeID]string{"page": filepath.Join(tmp, "write.md")},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(writeErr))

		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		recursiveChildErr := errors.New("child read failed")
		log := &recordingMigrationLogger{}
		err := addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID: map[migrationNodeID]string{
					"parent": `<!-- leafwiki
version: 1
page:
    id: parent
    title: Parent
    created_at: ""
    updated_at: ""
    creator_id: ""
    last_author_id: ""
-->
# Parent
`,
				},
				readErrByID: map[migrationNodeID]error{"child": recursiveChildErr},
			},
			Log: log,
		}, parent)

		Expect(err).To(MatchError(recursiveChildErr))
		Expect(log.errors).To(ContainElement("Error adding metadata to child node"))
	})

	ginkgo.It("backfillNodeMetadata covers nil, path, load, write, and child error branches", func() {
		Expect(backfillNodeMetadata(Dependencies{}, nil)).To(Succeed())

		page := &mutableMigrationNode{id: "page", title: "Page", kind: NodeKindPage}
		pathFailedErr := errors.New("path failed")
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathErrByID: map[migrationNodeID]error{"page": pathFailedErr},
			},
		}, page)).To(MatchError(pathFailedErr))

		tmp := ginkgo.GinkgoT().TempDir()
		invalidPath := filepath.Join(tmp, "invalid.md")
		Expect(os.WriteFile(invalidPath, []byte("<!-- leafwiki bad\n"), 0o644)).To(Succeed())
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID: map[migrationNodeID]string{"page": invalidPath},
			},
		}, page)).To(MatchError(markdown.ErrMetadataParse))

		originalWriteMarkdownFile := writeMarkdownFile
		metadataWriteErr := errors.New("write failed")
		writeMarkdownFile = func(*markdown.MarkdownFile) error {
			return metadataWriteErr
		}
		ginkgo.DeferCleanup(func() {
			writeMarkdownFile = originalWriteMarkdownFile
		})

		validPath := filepath.Join(tmp, "valid.md")
		Expect(os.WriteFile(validPath, []byte("# Page\n"), 0o644)).To(Succeed())
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID: map[migrationNodeID]string{"page": validPath},
			},
		}, page)).To(MatchError(metadataWriteErr))

		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		childPathFailedErr := errors.New("child path failed")
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID:    map[migrationNodeID]string{"parent": filepath.Join(tmp, "missing.md")},
				readPathErrByID: map[migrationNodeID]error{"child": childPathFailedErr},
			},
		}, parent)).To(MatchError(childPathFailedErr))
	})

	ginkgo.It("materializeSectionIndexes accepts nil roots", func() {
		Expect(materializeSectionIndexes(Dependencies{}, nil)).To(Succeed())
	})

	ginkgo.It("validateDependencies rejects a stored schema newer than the current schema", func() {
		deps := validDependencies()
		deps.CurrentSchemaVersion = 2

		err := Run(3, deps)

		Expect(err).To(MatchError(ErrStoredSchemaVersionNewer))
	})

	ginkgo.It("Run propagates SaveTree and SaveSchema errors after successful migrations", func() {
		saveTreeErr := errors.New("save tree failed")
		deps := validDependencies()
		deps.CurrentSchemaVersion = 1
		deps.SaveTree = func() error { return saveTreeErr }
		Expect(Run(1, deps)).To(Succeed())
		err := Run(0, deps)
		Expect(err).To(MatchError(saveTreeErr))

		saveSchemaErr := errors.New("save schema failed")
		deps = validDependencies()
		deps.CurrentSchemaVersion = 1
		deps.SaveSchema = func(int) error { return saveSchemaErr }
		err = Run(0, deps)
		Expect(err).To(MatchError(saveSchemaErr))
	})

	ginkgo.It("formatMetadataTime returns empty zero time and UTC RFC3339 for non-zero time", func() {
		Expect(formatMetadataTime(time.Time{})).To(BeEmpty())

		ts := time.Date(2026, 6, 25, 10, 11, 12, 0, time.FixedZone("offset", 2*60*60))
		Expect(formatMetadataTime(ts)).To(Equal("2026-06-25T08:11:12Z"))
	})
})
