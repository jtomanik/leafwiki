package wikid

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite3 "modernc.org/sqlite/lib"
)

var _ = ginkgo.Describe("wikid persistence and private route edge behavior", func() {
	ginkgo.Describe("registry and grant stores", func() {
		ginkgo.It("returns open errors before loading or mutating stores", ginkgo.Label("integration"), func() {
			badPath := wikidDBPathInsideFile()

			Expect(NewRegistryStore(badPath).Load()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(NewRegistryStore(badPath).Save(NewRegistryDocument())).To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).Load()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).Save(NewGrantDocument())).To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).GrantsForSubject("user:1")).Error().To(MatchError(syscall.ENOTDIR))
		})

		ginkgo.It("deletes stale registry rows when saving an empty document", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			_, err := registry.RegisterWorkspace(RegisterWorkspaceRequest{
				DisplayName: "Docs",
				DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
				RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(NewRegistryStore(layout.DBPath).Save(NewRegistryDocument())).To(Succeed())

			loaded, err := NewRegistryStore(layout.DBPath).Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.Workspaces).To(BeEmpty())
		})

		ginkgo.It("rejects invalid grant documents and subject replacement inputs", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			store := NewGrantStore(layout.DBPath)

			Expect(store.Save(GrantDocument{})).To(MatchError(ErrGrantSchemaVersion))
			Expect(store.Save(GrantDocument{
				SchemaVersion: GrantSchemaVersion,
				Grants:        []Grant{{Subject: "user:missing", WorkspaceID: "missing", Role: GrantRoleViewer}},
			})).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
			Expect(store.ReplaceSubjectGrants(" \t ", nil)).To(MatchError(ErrGrantSubjectRequired))
			Expect(store.ReplaceSubjectGrants("user:1", []Grant{{Subject: "user:2", WorkspaceID: home.ID, Role: GrantRoleViewer}})).To(MatchError(ErrGrantSubjectMismatch))
			Expect(store.ReplaceSubjectGrants("user:1", []Grant{{WorkspaceID: home.ID, Role: GrantRole("owner")}})).To(MatchError(ErrUnknownGrantRole))
		})

		ginkgo.It("surfaces grant delete failures from SQLite triggers", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			store := NewGrantStore(layout.DBPath)
			Expect(store.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleViewer})).To(Succeed())
			db := mustOpenInitializedWikidDB(layout.DBPath)
			Expect(execRawSQL(db, `CREATE TRIGGER fail_grant_delete BEFORE DELETE ON workspace_grants BEGIN SELECT RAISE(FAIL, 'grant delete blocked'); END`)).To(Succeed())

			Expect(store.Save(NewGrantDocument())).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
			Expect(store.ReplaceSubjectGrants("user:1", nil)).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
		})

		ginkgo.It("reports invalid persisted grant rows while loading and listing grants", ginkgo.Label("integration"), func() {
			Expect(loadGrantDocument(context.Background(), closedSQLiteDB())).Error().To(MatchError(wikidClosedDatabaseError()))

			scanDB := rawGrantDB()
			Expect(execRawSQL(scanDB, `INSERT INTO workspace_grants (subject, workspace_id, role) VALUES ('', 'home', 'viewer')`)).To(Succeed())
			Expect(loadGrantDocument(context.Background(), scanDB)).Error().To(MatchError(ErrGrantSubjectRequired))

			badIDDB := rawGrantDB()
			Expect(execRawSQL(badIDDB, `INSERT INTO workspace_grants (subject, workspace_id, role) VALUES ('user:1', 'bad/id', 'viewer')`)).To(Succeed())
			Expect(loadGrantDocument(context.Background(), badIDDB)).Error().To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))

			badRoleDB := rawGrantDB()
			Expect(execRawSQL(badRoleDB, `INSERT INTO workspace_grants (subject, workspace_id, role) VALUES ('user:1', 'home', 'owner')`)).To(Succeed())
			Expect(loadGrantDocument(context.Background(), badRoleDB)).Error().To(MatchError(ErrUnknownGrantRole))

			layout := newWikidEdgeLayout()
			db := mustOpenInitializedWikidDB(layout.DBPath)
			Expect(execRawSQL(db, `INSERT INTO workspaces (id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at) VALUES ('bad/id', 'Bad', '/tmp/bad-data', '/tmp/bad-root', '', '2026-06-27T12:00:00Z', '2026-06-27T12:00:00Z')`)).To(Succeed())
			Expect(execRawSQL(db, `INSERT INTO workspace_grants (subject, workspace_id, role) VALUES ('user:1', 'bad/id', 'viewer')`)).To(Succeed())
			Expect(NewGrantStore(layout.DBPath).GrantsForSubject("user:1")).Error().To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))

			queryErrorLayout := newWikidEdgeLayout()
			queryErrorDB := rawSQLiteDBAt(queryErrorLayout.DBPath)
			Expect(execRawSQL(queryErrorDB, `CREATE TABLE workspace_grants (subject TEXT, workspace_id TEXT)`)).To(Succeed())
			Expect(queryErrorDB.Close()).To(Succeed())
			Expect(NewGrantStore(queryErrorLayout.DBPath).GrantsForSubject("user:1")).Error().To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			scanErrorLayout := newWikidEdgeLayout()
			scanErrorDB := rawSQLiteDBAt(scanErrorLayout.DBPath)
			Expect(execRawSQL(scanErrorDB, `CREATE TABLE workspace_grants (subject TEXT, workspace_id TEXT, role TEXT)`)).To(Succeed())
			Expect(execRawSQL(scanErrorDB, `INSERT INTO workspace_grants (subject, workspace_id, role) VALUES ('user:1', 'bad/id', 'viewer')`)).To(Succeed())
			Expect(scanErrorDB.Close()).To(Succeed())
			Expect(NewGrantStore(scanErrorLayout.DBPath).GrantsForSubject("user:1")).Error().To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))

			grantRowsErr := errors.New("grant rows failed")
			Expect(grantsForSubjectRows(errRows{err: grantRowsErr})).Error().To(MatchError(grantRowsErr))
			loadGrantRowsErr := errors.New("load grant rows failed")
			Expect(loadGrantRows(errRows{err: loadGrantRowsErr})).Error().To(MatchError(loadGrantRowsErr))
		})

		ginkgo.It("rolls back registry updates when callbacks or next documents fail", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			store := NewRegistryStore(layout.DBPath)
			callbackErr := errors.New("callback failed")

			_, err := store.Update(func(RegistryDocument) (RegistryDocument, error) {
				return RegistryDocument{}, callbackErr
			})
			Expect(err).To(MatchError(callbackErr))

			_, err = store.Update(func(RegistryDocument) (RegistryDocument, error) {
				return RegistryDocument{}, nil
			})
			Expect(err).To(MatchError(ErrRegistrySchemaVersion))
		})

		ginkgo.It("uses store defaults and grant callbacks while registering workspaces", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			store := NewRegistryStore(layout.DBPath)
			result, err := store.RegisterWorkspaceWithResultAndGrants(
				WorkspaceRecord{
					ID:          "docs",
					DisplayName: "Docs",
					DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
					RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
					CreatedAt:   time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
					UpdatedAt:   time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
				},
				nil,
				func(result RegisterWorkspaceResult) ([]Grant, error) {
					return []Grant{{Subject: "user:1", Role: GrantRoleEditor}}, nil
				},
			)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(matchCreatedWorkspaceRegistration(workspaceid.WorkspaceID("docs")))
			grants, err := NewGrantStore(layout.DBPath).GrantsForSubject("user:1")
			Expect(err).NotTo(HaveOccurred())
			Expect(grants).To(Equal([]Grant{{Subject: "user:1", WorkspaceID: result.Workspace.ID, Role: GrantRoleEditor}}))
		})

		ginkgo.It("returns grant callback and grant workspace validation errors", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			store := NewRegistryStore(layout.DBPath)
			workspace := WorkspaceRecord{
				ID:          "docs",
				DisplayName: "Docs",
				DataDir:     filepath.Join(wikidTestTempDir(), "docs-data"),
				RootDir:     filepath.Join(wikidTestTempDir(), "docs-root"),
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			}
			callbackErr := errors.New("grant lookup failed")

			_, err := store.RegisterWorkspaceWithResultAndGrants(workspace, time.Now, func(RegisterWorkspaceResult) ([]Grant, error) {
				return nil, callbackErr
			})
			Expect(err).To(MatchError(callbackErr))

			workspace.ID = "docs-two"
			workspace.DataDir = filepath.Join(wikidTestTempDir(), "docs-two-data")
			workspace.RootDir = filepath.Join(wikidTestTempDir(), "docs-two-root")
			_, err = store.RegisterWorkspaceWithResultAndGrants(workspace, time.Now, func(RegisterWorkspaceResult) ([]Grant, error) {
				return []Grant{{Subject: "user:1", WorkspaceID: "bad/id", Role: GrantRoleViewer}}, nil
			})
			Expect(err).To(matchWikidWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))
		})

		ginkgo.It("normalizes replace helpers and workspace slugs", ginkgo.Label("unit"), func() {
			now := time.Now().UTC()
			doc := replaceWorkspaceRecord(NewRegistryDocument(), WorkspaceRecord{
				ID:          HomeWorkspaceID,
				DisplayName: " Home ",
				DataDir:     " /tmp/home-data ",
				RootDir:     " /tmp/home-root ",
				CreatedAt:   now,
				UpdatedAt:   now,
			})

			Expect(doc.Workspaces).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"DisplayName": Equal("Home"),
				"DataDir":     Equal("/tmp/home-data"),
			})))
			doc = replaceWorkspaceRecord(doc, WorkspaceRecord{
				ID:          HomeWorkspaceID,
				DisplayName: "Home Renamed",
				DataDir:     "/tmp/home-data",
				RootDir:     "/tmp/home-root",
				CreatedAt:   now,
				UpdatedAt:   now,
			})
			Expect(doc.Workspaces).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"DisplayName": Equal("Home Renamed"),
			})))
			Expect(workspaceSlug(" -_- ")).To(Equal("workspace"))
			workspaceID, err := workspaceIDFor("Docs", "/tmp/data", "/tmp/root")
			Expect(err).NotTo(HaveOccurred())
			Expect(workspaceID.StorageKey()).To(HavePrefix("docs-"))
		})

		ginkgo.It("reports invalid direct store registration inputs before opening a transaction", ginkgo.Label("unit"), func() {
			store := NewRegistryStore(filepath.Join(wikidTestTempDir(), "wikid.db"))

			_, err := store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{ID: "bad/id"}, time.Now, nil)
			Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))

			_, err = store.RegisterWorkspaceWithResultAndGrants(WorkspaceRecord{
				ID:                     HomeWorkspaceID,
				DataDir:                "/tmp/data",
				RootDir:                "/tmp/root",
				MarkdownLinkRootPrefix: "docs/",
			}, time.Now, nil)
			Expect(err).To(MatchError(ErrWorkspaceMarkdownLinkRootPrefixNotNormalized))
		})

		ginkgo.It("updates an existing home workspace and surfaces registry service load errors", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := service.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			updated, err := service.BootstrapHomeWorkspace(filepath.Join(wikidTestTempDir(), "new-data"), filepath.Join(wikidTestTempDir(), "new-root"))
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":      Equal(home.ID),
				"DataDir": Not(Equal(home.DataDir)),
			}))

			badService := NewRegistryService(NewRegistryStore(wikidDBPathInsideFile()), Layout{})
			Expect(badService.BootstrapHome()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(badService.RegisterWorkspace(RegisterWorkspaceRequest{DataDir: filepath.Join(wikidTestTempDir(), "data"), RootDir: filepath.Join(wikidTestTempDir(), "root")})).Error().To(MatchError(syscall.ENOTDIR))
			Expect(badService.ListWorkspaces()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(badService.Workspace(HomeWorkspaceID)).Error().To(MatchError(syscall.ENOTDIR))
			Expect(service.RegisterWorkspaceWithResultAndGrants(RegisterWorkspaceRequest{
				DisplayName:            "Bad Prefix",
				DataDir:                filepath.Join(wikidTestTempDir(), "bad-prefix-data"),
				RootDir:                filepath.Join(wikidTestTempDir(), "bad-prefix-root"),
				MarkdownLinkRootPrefix: "../docs",
			}, nil)).Error().To(MatchError(markdownlinks.ErrMarkdownLinkRootPrefixTraversal))
		})

		ginkgo.It("handles workspace request defaults, ordering ties, and path/prefix errors", ginkgo.Label("integration"), func() {
			layout := newWikidEdgeLayout()
			service := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			record, err := service.workspaceRecordForRequest(RegisterWorkspaceRequest{
				DataDir: filepath.Join(wikidTestTempDir(), "data"),
				RootDir: filepath.Join(wikidTestTempDir(), "root"),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(record.DisplayName).To(Equal("Workspace"))
			Expect(record.ID.StorageKey()).To(HavePrefix("workspace-"))

			blocker := filepath.Join(wikidTestTempDir(), "not-a-dir")
			Expect(os.WriteFile(blocker, []byte("x"), 0o644)).To(Succeed())
			_, err = service.workspaceRecordForRequest(RegisterWorkspaceRequest{DataDir: filepath.Join(blocker, "data"), RootDir: filepath.Join(wikidTestTempDir(), "root")})
			Expect(err).To(MatchError(syscall.ENOTDIR))
			_, err = service.workspaceRecordForRequest(RegisterWorkspaceRequest{DataDir: filepath.Join(wikidTestTempDir(), "data"), RootDir: filepath.Join(blocker, "root")})
			Expect(err).To(MatchError(syscall.ENOTDIR))
			_, err = service.workspaceRecordForRequest(RegisterWorkspaceRequest{DataDir: filepath.Join(wikidTestTempDir(), "data"), RootDir: filepath.Join(wikidTestTempDir(), "root"), MarkdownLinkRootPrefix: "../docs"})
			Expect(err).To(MatchError(markdownlinks.ErrMarkdownLinkRootPrefixTraversal))

			_, err = service.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			_, err = service.RegisterWorkspace(RegisterWorkspaceRequest{DisplayName: "Same", DataDir: filepath.Join(wikidTestTempDir(), "b-data"), RootDir: filepath.Join(wikidTestTempDir(), "b-root")})
			Expect(err).NotTo(HaveOccurred())
			_, err = service.RegisterWorkspace(RegisterWorkspaceRequest{DisplayName: "same", DataDir: filepath.Join(wikidTestTempDir(), "a-data"), RootDir: filepath.Join(wikidTestTempDir(), "a-root")})
			Expect(err).NotTo(HaveOccurred())
			workspaces, err := service.ListWorkspaces()
			Expect(err).NotTo(HaveOccurred())
			Expect(workspaces).To(haveHomeWorkspaceFirstAndEqualFoldTie())
		})

		ginkgo.It("reports invalid persisted registry rows while loading workspaces", ginkgo.Label("integration"), func() {
			Expect(loadRegistryDocument(context.Background(), closedSQLiteDB())).Error().To(MatchError(wikidClosedDatabaseError()))

			scanDB := rawRegistryDB()
			Expect(execRawSQL(scanDB, `INSERT INTO workspaces (id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at) VALUES ('home', 'Home', '', '/tmp/root', '', '2026-06-27T12:00:00Z', '2026-06-27T12:00:00Z')`)).To(Succeed())
			Expect(loadRegistryDocument(context.Background(), scanDB)).Error().To(MatchError(ErrWorkspaceDataDirRequired))

			createdAtDB := rawRegistryDB()
			Expect(execRawSQL(createdAtDB, `INSERT INTO workspaces (id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at) VALUES ('home', 'Home', '/tmp/data', '/tmp/root', '', 'bad-time', '2026-06-27T12:00:00Z')`)).To(Succeed())
			Expect(loadRegistryDocument(context.Background(), createdAtDB)).Error().To(matchWikidTimeParseError())

			updatedAtDB := rawRegistryDB()
			Expect(execRawSQL(updatedAtDB, `INSERT INTO workspaces (id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at) VALUES ('home', 'Home', '/tmp/data', '/tmp/root', '', '2026-06-27T12:00:00Z', 'bad-time')`)).To(Succeed())
			Expect(loadRegistryDocument(context.Background(), updatedAtDB)).Error().To(matchWikidTimeParseError())

			validateDB := rawRegistryDB()
			Expect(execRawSQL(validateDB, `INSERT INTO workspaces (id, display_name, data_dir, root_dir, markdown_link_root_prefix, created_at, updated_at) VALUES ('home', 'Home', '/tmp/data', '/tmp/root', 'docs/', '2026-06-27T12:00:00Z', '2026-06-27T12:00:00Z')`)).To(Succeed())
			Expect(loadRegistryDocument(context.Background(), validateDB)).Error().To(MatchError(ErrWorkspaceMarkdownLinkRootPrefixNotNormalized))

			registryRowsErr := errors.New("registry rows failed")
			Expect(loadRegistryRows(errRows{err: registryRowsErr})).Error().To(MatchError(registryRowsErr))
		})

		ginkgo.It("surfaces low-level registry save and upsert errors", ginkgo.Label("integration"), func() {
			db := rawSQLiteDB()
			conn, err := db.Conn(context.Background())
			Expect(err).NotTo(HaveOccurred())
			ginkgo.DeferCleanup(conn.Close)

			err = saveRegistryDocument(context.Background(), conn, RegistryDocument{
				SchemaVersion: RegistrySchemaVersion,
				Workspaces:    []WorkspaceRecord{testWorkspaceRecord("edge-save")},
			})
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			err = upsertWorkspace(context.Background(), conn, WorkspaceRecord{ID: "bad/id", DataDir: "/tmp/data", RootDir: "/tmp/root"})
			Expect(err).To(WithTransform(workspaceid.WorkspaceIDErrorCode, Equal(workspaceid.ErrCodeWorkspaceIDInvalid)))
		})

		ginkgo.It("surfaces registry transaction load and upsert failures", ginkgo.Label("integration"), func() {
			badSchemaLayout := newWikidEdgeLayout()
			badSchemaDB := rawSQLiteDBAt(badSchemaLayout.DBPath)
			Expect(execRawSQL(badSchemaDB, `CREATE TABLE workspaces (id TEXT)`)).To(Succeed())
			Expect(badSchemaDB.Close()).To(Succeed())
			_, err := NewRegistryStore(badSchemaLayout.DBPath).Update(func(doc RegistryDocument) (RegistryDocument, error) {
				return doc, nil
			})
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			registerLoadLayout := newWikidEdgeLayout()
			registerLoadDB := rawSQLiteDBAt(registerLoadLayout.DBPath)
			Expect(execRawSQL(registerLoadDB, `CREATE TABLE workspaces (id TEXT)`)).To(Succeed())
			Expect(registerLoadDB.Close()).To(Succeed())
			_, err = NewRegistryStore(registerLoadLayout.DBPath).RegisterWorkspaceWithResultAndGrants(testWorkspaceRecord("load-fail"), time.Now, nil)
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			insertFailLayout := newWikidEdgeLayout()
			insertFailDB := mustOpenInitializedWikidDB(insertFailLayout.DBPath)
			Expect(execRawSQL(insertFailDB, `CREATE TRIGGER fail_workspace_insert BEFORE INSERT ON workspaces BEGIN SELECT RAISE(FAIL, '`+wikidWorkspaceInsertBlockedFixture+`'); END`)).To(Succeed())
			_, err = NewRegistryStore(insertFailLayout.DBPath).RegisterWorkspaceWithResultAndGrants(testWorkspaceRecord("insert-fail"), time.Now, nil)
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))

			updateFailLayout := newWikidEdgeLayout()
			updateStore := NewRegistryStore(updateFailLayout.DBPath)
			workspace := testWorkspaceRecord("update-fail")
			_, err = updateStore.RegisterWorkspaceWithResultAndGrants(workspace, time.Now, nil)
			Expect(err).NotTo(HaveOccurred())
			updateFailDB := mustOpenInitializedWikidDB(updateFailLayout.DBPath)
			Expect(execRawSQL(updateFailDB, `CREATE TRIGGER fail_workspace_update BEFORE UPDATE ON workspaces BEGIN SELECT RAISE(FAIL, '`+wikidWorkspaceUpdateBlockedFixture+`'); END`)).To(Succeed())
			_, err = updateStore.RegisterWorkspaceWithResultAndGrants(workspace, nil, nil)
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_CONSTRAINT))
		})
	})
})
