package treemigration

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/markdown"
)

type mutableMigrationNode struct {
	id       string
	title    string
	slug     string
	kind     string
	metadata Metadata
	children []Node
}

func (n *mutableMigrationNode) ID() string                { return n.id }
func (n *mutableMigrationNode) Title() string             { return n.title }
func (n *mutableMigrationNode) Slug() string              { return n.slug }
func (n *mutableMigrationNode) Kind() string              { return n.kind }
func (n *mutableMigrationNode) SetKind(kind string)       { n.kind = kind }
func (n *mutableMigrationNode) Metadata() Metadata        { return n.metadata }
func (n *mutableMigrationNode) SetMetadata(meta Metadata) { n.metadata = meta }
func (n *mutableMigrationNode) Children() []Node          { return n.children }

type configurableMigrationStore struct {
	resolved       *ResolvedNode
	resolvedByID   map[string]*ResolvedNode
	resolveErr     error
	resolveErrByID map[string]error
	resolveN       int

	readPath        string
	readPathByID    map[string]string
	readPathErr     error
	readPathErrByID map[string]error

	writePath        string
	writePathByID    map[string]string
	writePathErr     error
	writePathErrByID map[string]error

	readRaw        string
	readRawByID    map[string]string
	readErr        error
	readErrByID    map[string]error
	saveErrByID    map[string]error
	savedOrderIDs  []string
	ensureErrByID  map[string]error
	ensuredIndexID []string
}

func migrationNodeID(node Node) string {
	if node == nil {
		return ""
	}
	return node.ID()
}

func (s *configurableMigrationStore) ResolveNode(node Node) (*ResolvedNode, error) {
	s.resolveN++
	id := migrationNodeID(node)
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
	id := migrationNodeID(node)
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
	id := migrationNodeID(node)
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
	id := migrationNodeID(node)
	if err, ok := s.ensureErrByID[id]; ok {
		return "", err
	}
	s.ensuredIndexID = append(s.ensuredIndexID, id)
	return "", nil
}

func (s *configurableMigrationStore) ReadPageRaw(node Node) (string, error) {
	id := migrationNodeID(node)
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
	id := migrationNodeID(node)
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
		Expect(meta.CreatedAt).To(Equal(modTime.UTC()))
		Expect(meta.UpdatedAt).To(Equal(modTime.UTC()))
		Expect(meta.CreatorID).To(Equal("alice"))
		Expect(meta.LastAuthorID).To(Equal("bob"))
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

		Expect(store.resolveN).To(Equal(0))
		Expect(node.Metadata().CreatedAt).To(Equal(existing))
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
		store := &configurableMigrationStore{
			saveErrByID: map[string]error{"child": errors.New("save failed")},
		}

		err := backfillChildOrder(Dependencies{Store: store, Log: &recordingMigrationLogger{}}, root)

		Expect(err).To(MatchError(ContainSubstring("persist child order for node child")))
		Expect(store.savedOrderIDs).To(Equal([]string{"root", "child"}))
	})

	ginkgo.It("migrateToV2 logs and returns managed metadata failures for root children", func() {
		child := &mutableMigrationNode{id: "page-1", kind: NodeKindPage}
		root := &mutableMigrationNode{id: "root", kind: NodeKindSection, children: []Node{child}}
		store := &configurableMigrationStore{
			readErrByID: map[string]error{"page-1": errors.New("read failed")},
		}
		log := &recordingMigrationLogger{}

		err := migrateToV2(Dependencies{Root: root, Store: store, Log: log})

		Expect(err).To(MatchError(ContainSubstring("could not read page content for node page-1")))
		Expect(log.errors).To(ContainElements(
			"Could not read page content for node",
			"Error adding metadata to child node",
		))
	})

	ginkgo.It("backfillKindFromFS resolves unknown kinds and falls back to child-aware heuristics", func() {
		Expect(func() { backfillKindFromFS(Dependencies{}, nil) }).ToNot(Panic())

		resolved := &mutableMigrationNode{id: "resolved", kind: "legacy"}
		grandchild := &mutableMigrationNode{id: "grandchild", kind: NodeKindPage}
		heuristicSection := &mutableMigrationNode{id: "section", kind: "legacy", children: []Node{grandchild}}
		heuristicPage := &mutableMigrationNode{id: "page", kind: "legacy"}
		root := &mutableMigrationNode{
			id:       "root",
			kind:     "legacy",
			children: []Node{nil, resolved, heuristicSection, heuristicPage},
		}
		store := &configurableMigrationStore{
			resolvedByID: map[string]*ResolvedNode{
				"resolved": {Kind: NodeKindSection},
			},
			resolveErrByID: map[string]error{
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
			readErrByID:   map[string]error{"parent": missingContentErr},
			readRawByID:   map[string]string{"child": "# Child\n"},
			writePathByID: map[string]string{"child": childPath},
		}
		log := &recordingMigrationLogger{}

		err := addManagedMetadata(Dependencies{
			Store:               store,
			Log:                 log,
			IsMissingContentErr: func(err error) bool { return errors.Is(err, missingContentErr) },
		}, parent)

		Expect(err).ToNot(HaveOccurred())
		Expect(log.warnings).To(ContainElement("Page file does not exist, skipping metadata addition"))
		Expect(log.infos).To(ContainElement("metadata backfilled"))
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
			readErrByID: map[string]error{
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

		Expect(err).To(MatchError(ContainSubstring("could not read page content for node child")))
		Expect(log.errors).To(ContainElement("Error adding metadata to child node"))
	})

	ginkgo.It("addManagedMetadata reports write-path, parse, write, and recursive child failures", func() {
		tmp := ginkgo.GinkgoT().TempDir()
		page := &mutableMigrationNode{id: "page", title: "Page", kind: NodeKindPage}

		pathErr := errors.New("path failed")
		Expect(addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID:      map[string]string{"page": "# Page\n"},
				writePathErrByID: map[string]error{"page": pathErr},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(ContainSubstring("could not determine content path for node page")))

		Expect(addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID:   map[string]string{"page": "---\ninvalid: [\n---\n# Page\n"},
				writePathByID: map[string]string{"page": filepath.Join(tmp, "parse.md")},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(ContainSubstring("could not parse markdown content for node page")))

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
				readRawByID:   map[string]string{"page": "# Page\n"},
				writePathByID: map[string]string{"page": filepath.Join(tmp, "write.md")},
			},
			Log: &recordingMigrationLogger{},
		}, page)).To(MatchError(ContainSubstring("could not write updated page content for node page")))

		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		log := &recordingMigrationLogger{}
		err := addManagedMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readRawByID: map[string]string{
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
				readErrByID: map[string]error{"child": errors.New("child read failed")},
			},
			Log: log,
		}, parent)

		Expect(err).To(MatchError(ContainSubstring("could not read page content for node child")))
		Expect(log.errors).To(ContainElement("Error adding metadata to child node"))
	})

	ginkgo.It("backfillNodeMetadata covers nil, path, load, write, and child error branches", func() {
		Expect(backfillNodeMetadata(Dependencies{}, nil)).To(Succeed())

		page := &mutableMigrationNode{id: "page", title: "Page", kind: NodeKindPage}
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathErrByID: map[string]error{"page": errors.New("path failed")},
			},
		}, page)).To(MatchError(ContainSubstring("could not determine content path for node page")))

		tmp := ginkgo.GinkgoT().TempDir()
		invalidPath := filepath.Join(tmp, "invalid.md")
		Expect(os.WriteFile(invalidPath, []byte("<!-- leafwiki bad\n"), 0o644)).To(Succeed())
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID: map[string]string{"page": invalidPath},
			},
		}, page)).To(MatchError(ContainSubstring("could not load markdown file for node page")))

		originalWriteMarkdownFile := writeMarkdownFile
		writeMarkdownFile = func(*markdown.MarkdownFile) error {
			return errors.New("write failed")
		}
		ginkgo.DeferCleanup(func() {
			writeMarkdownFile = originalWriteMarkdownFile
		})

		validPath := filepath.Join(tmp, "valid.md")
		Expect(os.WriteFile(validPath, []byte("# Page\n"), 0o644)).To(Succeed())
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID: map[string]string{"page": validPath},
			},
		}, page)).To(MatchError(ContainSubstring("could not write migrated metadata for node page")))

		child := &mutableMigrationNode{id: "child", title: "Child", kind: NodeKindPage}
		parent := &mutableMigrationNode{id: "parent", title: "Parent", kind: NodeKindSection, children: []Node{child}}
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathByID:    map[string]string{"parent": filepath.Join(tmp, "missing.md")},
				readPathErrByID: map[string]error{"child": errors.New("child path failed")},
			},
		}, parent)).To(MatchError(ContainSubstring("could not determine content path for node child")))
	})

	ginkgo.It("materializeSectionIndexes accepts nil roots", func() {
		Expect(materializeSectionIndexes(Dependencies{}, nil)).To(Succeed())
	})

	ginkgo.It("validateDependencies rejects a stored schema newer than the current schema", func() {
		deps := validDependencies()
		deps.CurrentSchemaVersion = 2

		err := Run(3, deps)

		Expect(err).To(MatchError(ContainSubstring("current schema version 2 is older than stored version 3")))
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
