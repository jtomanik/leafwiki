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

var _ = ginkgo.Describe("tree migration metadata backfill", ginkgo.Label("unit"), func() {
	ginkgo.It("uses filesystem timestamps for missing metadata while preserving author identity", func() {
		tmp := tempMigrationScratchDir()
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

	ginkgo.It("leaves nodes unchanged when metadata already exists", func() {
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

	ginkgo.It("logs unresolved nodes as non-fatal metadata misses", func() {
		node := &mutableMigrationNode{id: "page-1"}
		store := &configurableMigrationStore{resolveErr: errors.New("resolve failed")}
		log := &recordingMigrationLogger{}

		Expect(backfillMetadata(Dependencies{Store: store, Log: log}, node)).To(Succeed())

		Expect(node.Metadata()).To(matchMigrationMetadataTimestampState(migrationMetadataTimestampsMissing))
		Expect(log.errors).To(ContainElement("Could not resolve node for metadata backfill"))
	})

	ginkgo.It("skips nil nodes and falls back to current time when stat fails", func() {
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

		Expect(node.Metadata()).To(matchMigrationMetadataTimestampState(migrationMetadataTimestampsBackfilled))
		Expect(child.Metadata()).To(matchMigrationMetadataTimestampState(migrationMetadataTimestampsBackfilled))
		Expect(log.errors).To(ContainElement("Could not stat node for metadata"))
	})

	ginkgo.It("orders nested children and surfaces child order persistence failures", func() {
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

	ginkgo.It("logs managed metadata failures from root children and returns the child error", func() {
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

	ginkgo.It("infers unknown node kinds from filesystem resolution and child structure", func() {
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

	ginkgo.It("continues into children when a section parent has missing content", func() {
		missingContentErr := errors.New("missing content")
		childPath := filepath.Join(tempMigrationScratchDir(), "child.md")
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

	ginkgo.It("logs child metadata failures from missing-content parents", func() {
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

	ginkgo.It("surfaces write-path, parse, write, and recursive child metadata failures", func() {
		tmp := tempMigrationScratchDir()
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

	ginkgo.It("handles nil nodes and surfaces path, load, write, and child metadata errors", func() {
		Expect(backfillNodeMetadata(Dependencies{}, nil)).To(Succeed())

		page := &mutableMigrationNode{id: "page", title: "Page", kind: NodeKindPage}
		pathFailedErr := errors.New("path failed")
		Expect(backfillNodeMetadata(Dependencies{
			Store: &configurableMigrationStore{
				readPathErrByID: map[migrationNodeID]error{"page": pathFailedErr},
			},
		}, page)).To(MatchError(pathFailedErr))

		tmp := tempMigrationScratchDir()
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

	ginkgo.It("accepts nil section roots", func() {
		Expect(materializeSectionIndexes(Dependencies{}, nil)).To(Succeed())
	})

	ginkgo.It("rejects stored schemas newer than the supported schema", func() {
		deps := validDependencies()
		deps.CurrentSchemaVersion = 2

		err := Run(3, deps)

		Expect(err).To(MatchError(ErrStoredSchemaVersionNewer))
	})

	ginkgo.It("propagates save-tree and save-schema failures after successful migrations", func() {
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

	ginkgo.It("formats zero metadata timestamps as empty and non-zero timestamps as UTC RFC3339", func() {
		Expect(formatMetadataTime(time.Time{})).To(BeEmpty())

		ts := time.Date(2026, 6, 25, 10, 11, 12, 0, time.FixedZone("offset", 2*60*60))
		Expect(formatMetadataTime(ts)).To(Equal("2026-06-25T08:11:12Z"))
	})
})
