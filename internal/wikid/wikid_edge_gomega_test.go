package wikid

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"
	"unsafe"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/frontd"
	"github.com/perber/wiki/internal/projectdaemon"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspaceid"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const (
	wikidWorkspaceInsertBlockedFixture = "workspace insert blocked"
	wikidWorkspaceUpdateBlockedFixture = "workspace update blocked"
)

type wikidErrorMatcher struct {
	label string
	match func(error) bool
}

func matchWikidWorkspaceIDValidationError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return wikidErrorMatcher{
		label: "workspace ID validation error",
		match: func(err error) bool {
			var validationErr *workspaceid.ValidationError
			return errors.As(err, &validationErr) &&
				validationErr.Code == code &&
				validationErr.MessageID == sharederrors.MessageIDForCode(code)
		},
	}
}

func (matcher wikidErrorMatcher) Match(actual interface{}) (bool, error) {
	err, ok := actual.(error)
	if !ok {
		return false, nil
	}
	return matcher.match(err), nil
}

func (matcher wikidErrorMatcher) FailureMessage(actual interface{}) string {
	return "Expected\n\t" + format.Object(actual, 1) + "\nto satisfy " + matcher.label
}

func (matcher wikidErrorMatcher) NegatedFailureMessage(actual interface{}) string {
	return "Expected\n\t" + format.Object(actual, 1) + "\nnot to satisfy " + matcher.label
}

var _ = ginkgo.Describe("wikid persistence and private route edge behavior", func() {
	ginkgo.Describe("auth storage", func() {
		ginkgo.It("joins legacy cleanup errors and propagates auth directory setup failures", func() {
			dataDir := wikidTestTempDir()
			legacyUsersDB := filepath.Join(dataDir, "users.db")
			Expect(os.Mkdir(legacyUsersDB, 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(legacyUsersDB, "child"), []byte("x"), 0o644)).To(Succeed())

			Expect(CleanupLegacyAuthDBs(dataDir)).To(MatchError(syscall.ENOTEMPTY))
			Expect(OpenAuthStores(dataDir)).Error().To(MatchError(syscall.ENOTEMPTY))

			blockedAuthDirData := wikidTestTempDir()
			Expect(os.Mkdir(filepath.Join(blockedAuthDirData, ".leafwiki"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(blockedAuthDirData, ".leafwiki", "wikid"), []byte("x"), 0o644)).To(Succeed())
			Expect(OpenAuthStores(blockedAuthDirData)).Error().To(MatchError(syscall.ENOTDIR))

			blockedOAuthDirData := wikidTestTempDir()
			wikidDir := filepath.Join(blockedOAuthDirData, ".leafwiki", "wikid")
			Expect(os.MkdirAll(filepath.Join(wikidDir, "auth"), 0o755)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(wikidDir, "oauth"), []byte("x"), 0o644)).To(Succeed())
			Expect(OpenAuthStores(blockedOAuthDirData)).Error().To(MatchError(syscall.ENOTDIR))
		})

		ginkgo.It("treats nil auth store collections as already closed", func() {
			var stores *AuthStores
			Expect(stores.Close()).To(Succeed())
		})

		ginkgo.It("propagates auth store constructor failures after directory setup succeeds", func() {
			restore := captureWikidAuthSeams()
			ginkgo.DeferCleanup(restore)

			userStoreErr := errors.New("user store failed")
			wikidNewUserStore = func(string) (*coreauth.UserStore, error) {
				return nil, userStoreErr
			}
			Expect(OpenAuthStores(wikidTestTempDir())).Error().To(MatchError(userStoreErr))

			restore()
			restore = captureWikidAuthSeams()
			ginkgo.DeferCleanup(restore)
			sessionStoreErr := errors.New("session store failed")
			wikidNewSessionStore = func(string) (*coreauth.SessionStore, error) {
				return nil, sessionStoreErr
			}
			Expect(OpenAuthStores(wikidTestTempDir())).Error().To(MatchError(sessionStoreErr))

			restore()
			restore = captureWikidAuthSeams()
			ginkgo.DeferCleanup(restore)
			apiKeyStoreErr := errors.New("api key store failed")
			wikidNewAPIKeyStore = func(string) (*coreauth.APIKeyStore, error) {
				return nil, apiKeyStoreErr
			}
			Expect(OpenAuthStores(wikidTestTempDir())).Error().To(MatchError(apiKeyStoreErr))
		})
	})

	ginkgo.Describe("document validation", func() {
		ginkgo.It("reports registry document and workspace record validation failures", func() {
			now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
			valid := WorkspaceRecord{
				ID:          HomeWorkspaceID,
				DisplayName: "Home",
				DataDir:     "/tmp/home-data",
				RootDir:     "/tmp/home-root",
				CreatedAt:   now,
				UpdatedAt:   now,
			}

			Expect((RegistryDocument{}).Validate()).To(MatchError(ErrRegistrySchemaVersion))
			Expect(RegistryDocument{
				SchemaVersion: RegistrySchemaVersion,
				Workspaces: []WorkspaceRecord{
					valid,
					{ID: HomeWorkspaceID, DisplayName: "Other", DataDir: "/tmp/other-data", RootDir: "/tmp/other-root", CreatedAt: now, UpdatedAt: now},
				},
			}.Validate()).To(MatchError(ErrDuplicateWorkspaceID))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: "bad/id", DataDir: "/tmp/data", RootDir: "/tmp/root"})).To(testmatchers.HaveStructuredError(workspaceid.ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(workspaceid.ErrCodeWorkspaceIDInvalid)))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, RootDir: "/tmp/root"})).To(MatchError(ErrWorkspaceDataDirRequired))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data"})).To(MatchError(ErrWorkspaceRootDirRequired))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data", RootDir: "/tmp/root", MarkdownLinkRootPrefix: "docs/"})).To(MatchError(ErrWorkspaceMarkdownLinkRootPrefixNotNormalized))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data", RootDir: "/tmp/root", MarkdownLinkRootPrefix: "../docs"})).To(MatchError(markdownlinks.ErrMarkdownLinkRootPrefixTraversal))
		})

		ginkgo.It("reports grant document validation failures", func() {
			Expect((GrantDocument{}).Validate()).To(MatchError(ErrGrantSchemaVersion))
			Expect(validateGrant(Grant{WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(MatchError(ErrGrantSubjectRequired))
			Expect(validateGrant(Grant{Subject: "user:1", WorkspaceID: "bad/id", Role: GrantRoleViewer})).To(matchWikidWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))
			Expect(validateGrant(Grant{Subject: "user:1", WorkspaceID: HomeWorkspaceID, Role: GrantRole("owner")})).To(MatchError(ErrUnknownGrantRole))
			Expect(CapabilitiesForRole(GrantRole("owner"))).Error().To(MatchError(ErrUnknownGrantRole))
		})

		ginkgo.It("canonicalizes paths through existing symlink ancestors and missing suffixes", func() {
			root := wikidTestTempDir()
			realDir := filepath.Join(root, "real")
			linkDir := filepath.Join(root, "link")
			Expect(os.Mkdir(realDir, 0o755)).To(Succeed())
			Expect(os.Symlink(realDir, linkDir)).To(Succeed())

			existing, err := canonicalRegistryPath(realDir)
			Expect(err).NotTo(HaveOccurred())
			resolvedRealDir, resolveErr := filepath.EvalSymlinks(realDir)
			Expect(resolveErr).NotTo(HaveOccurred())
			Expect(existing).To(Equal(resolvedRealDir))

			got, err := canonicalRegistryPath(filepath.Join(linkDir, "missing", "child"))

			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(filepath.Join(resolvedRealDir, "missing", "child")))
		})

		ginkgo.It("surfaces canonical path resolver failures", func() {
			restore := captureWikidPathSeams()
			ginkgo.DeferCleanup(restore)
			absErr := errors.New("abs failed")
			wikidFilepathAbs = func(string) (string, error) {
				return "", absErr
			}
			Expect(canonicalRegistryPath("docs")).Error().To(MatchError(absErr))

			restore()
			restore = captureWikidPathSeams()
			ginkgo.DeferCleanup(restore)
			permissionErr := errors.New("permission denied")
			wikidEvalSymlinks = func(string) (string, error) {
				return "", permissionErr
			}
			Expect(canonicalRegistryPath("/tmp/docs")).Error().To(MatchError(permissionErr))

			restore()
			restore = captureWikidPathSeams()
			ginkgo.DeferCleanup(restore)
			wikidEvalSymlinks = func(path string) (string, error) {
				if path == "/" {
					return "", os.ErrNotExist
				}
				return "", os.ErrNotExist
			}
			got, err := canonicalRegistryPath("/missing/path")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal("/missing/path"))

			restore()
			restore = captureWikidPathSeams()
			ginkgo.DeferCleanup(restore)
			calls := 0
			ancestorPermissionErr := errors.New("ancestor permission denied")
			wikidEvalSymlinks = func(string) (string, error) {
				calls++
				if calls == 1 {
					return "", os.ErrNotExist
				}
				return "", ancestorPermissionErr
			}
			Expect(canonicalRegistryPath("/tmp/missing/path")).Error().To(MatchError(ancestorPermissionErr))
		})
	})

	ginkgo.Describe("registry and grant stores", func() {
		ginkgo.It("returns open errors before loading or mutating stores", func() {
			badPath := wikidDBPathInsideFile()

			Expect(NewRegistryStore(badPath).Load()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(NewRegistryStore(badPath).Save(NewRegistryDocument())).To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).Load()).Error().To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).Save(NewGrantDocument())).To(MatchError(syscall.ENOTDIR))
			Expect(NewGrantStore(badPath).GrantsForSubject("user:1")).Error().To(MatchError(syscall.ENOTDIR))
		})

		ginkgo.It("deletes stale registry rows when saving an empty document", func() {
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

		ginkgo.It("rejects invalid grant documents and subject replacement inputs", func() {
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

		ginkgo.It("surfaces grant delete failures from SQLite triggers", func() {
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

		ginkgo.It("reports invalid persisted grant rows while loading and listing grants", func() {
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

		ginkgo.It("rolls back registry updates when callbacks or next documents fail", func() {
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

		ginkgo.It("uses store defaults and grant callbacks while registering workspaces", func() {
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
			Expect(result.Created).To(BeTrue())
			grants, err := NewGrantStore(layout.DBPath).GrantsForSubject("user:1")
			Expect(err).NotTo(HaveOccurred())
			Expect(grants).To(Equal([]Grant{{Subject: "user:1", WorkspaceID: result.Workspace.ID, Role: GrantRoleEditor}}))
		})

		ginkgo.It("returns grant callback and grant workspace validation errors", func() {
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

		ginkgo.It("normalizes replace helpers and workspace slugs", func() {
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

		ginkgo.It("reports invalid direct store registration inputs before opening a transaction", func() {
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

		ginkgo.It("updates an existing home workspace and surfaces registry service load errors", func() {
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

		ginkgo.It("handles workspace request defaults, ordering ties, and path/prefix errors", func() {
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

		ginkgo.It("reports invalid persisted registry rows while loading workspaces", func() {
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

		ginkgo.It("surfaces low-level registry save and upsert errors", func() {
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

		ginkgo.It("surfaces registry transaction load and upsert failures", func() {
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

	ginkgo.Describe("private workspace API", func() {
		ginkgo.It("routes not-found and malformed workspace paths before authorization", func() {
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{})

			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, "/elsewhere")).To(Equal(http.StatusNotFound))
			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, PrivateWorkspacesPrefix+"/missing")).To(Equal(http.StatusNotFound))
			Expect(privateWorkspaceAPIStatus(api, http.MethodGet, PrivateWorkspacesPrefix+"/bad/id/status")).To(Equal(http.StatusNotFound))
		})

		ginkgo.It("validates subjects for list requests", func() {
			for _, tc := range []struct {
				name    string
				subject func(*http.Request) (WorkspaceSubject, error)
			}{
				{name: "missing resolver"},
				{name: "resolver error", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{}, errors.New("no subject")
				}},
				{name: "blank subject", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: " \t "}, nil
				}},
				{name: "invalid role", subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: "user:1", Role: GrantRole("owner")}, nil
				}},
			} {
				tc := tc
				ginkgo.By(tc.name)
				api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
					Registry: NewRegistryService(NewRegistryStore(filepath.Join(wikidTestTempDir(), "wikid.db")), Layout{}),
					Grants:   NewGrantStore(filepath.Join(wikidTestTempDir(), "wikid.db")),
					Subject:  tc.subject,
				})

				rec := privateWorkspaceAPIRecorder(api, http.MethodGet, PrivateWorkspacesPrefix)

				Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateSubjectResolveFailed, sharederrors.MessageIDForCode(errCodePrivateSubjectResolveFailed)))
			}
		})

		ginkgo.It("reports registry and grant load failures separately", func() {
			registryErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(wikidDBPathInsideFile()), Layout{}),
				Subject:  validWorkspaceSubject,
			})
			registryRec := privateWorkspaceAPIRecorder(registryErrorAPI, http.MethodGet, PrivateWorkspacesPrefix)
			Expect(registryRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateRegistryLoadFailed, sharederrors.MessageIDForCode(errCodePrivateRegistryLoadFailed)))

			layout := newWikidEdgeLayout()
			Expect(NewRegistryService(NewRegistryStore(layout.DBPath), layout).BootstrapHome()).Error().NotTo(HaveOccurred())
			grantsErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(layout.DBPath), layout),
				Grants:   NewGrantStore(wikidDBPathInsideFile()),
				Subject:  validWorkspaceSubject,
			})
			grantsRec := privateWorkspaceAPIRecorder(grantsErrorAPI, http.MethodGet, PrivateWorkspacesPrefix)
			Expect(grantsRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateGrantsLoadFailed, sharederrors.MessageIDForCode(errCodePrivateGrantsLoadFailed)))
		})

		ginkgo.It("handles status and default ensure branches without a supervisor", func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			grants := NewGrantStore(layout.DBPath)
			Expect(grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleViewer})).To(Succeed())
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   grants,
				Subject:  validWorkspaceSubject,
			})

			statusRec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodGet, home.ID, "status")
			Expect(statusRec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateRegistered))),
			))

			ensureRec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodPost, home.ID, "ensure")
			Expect(ensureRec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateRegistered))),
			))
		})

		ginkgo.It("returns supervisor status from default ensure when available", func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())
			grants := NewGrantStore(layout.DBPath)
			Expect(grants.Upsert(Grant{Subject: "user:1", WorkspaceID: home.ID, Role: GrantRoleEditor})).To(Succeed())
			supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{})
			supervisor.MarkStarting(home.ID)
			api := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry:   registry,
				Grants:     grants,
				Subject:    validWorkspaceSubject,
				Supervisor: supervisor,
			})

			rec := privateWorkspaceAPIRecorderForWorkspace(api, http.MethodPost, home.ID, "ensure")

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody(ContainSubstring(string(WorkspaceStateStarting))),
			))
		})

		ginkgo.It("reports authorization and ensure failures from workspace actions", func() {
			layout := newWikidEdgeLayout()
			registry := NewRegistryService(NewRegistryStore(layout.DBPath), layout)
			home, err := registry.BootstrapHome()
			Expect(err).NotTo(HaveOccurred())

			subjectErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(layout.DBPath),
				Subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{}, errors.New("subject failed")
				},
			})
			subjectRec := privateWorkspaceAPIRecorderForWorkspace(subjectErrorAPI, http.MethodGet, home.ID, "status")
			Expect(subjectRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			grantsErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(wikidDBPathInsideFile()),
				Subject:  validWorkspaceSubject,
			})
			grantsRec := privateWorkspaceAPIRecorderForWorkspace(grantsErrorAPI, http.MethodGet, home.ID, "status")
			Expect(grantsRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			registryErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: NewRegistryService(NewRegistryStore(wikidDBPathInsideFile()), Layout{}),
				Grants:   NewGrantStore(layout.DBPath),
				Subject:  validWorkspaceSubject,
			})
			registryRec := privateWorkspaceAPIRecorderForWorkspace(registryErrorAPI, http.MethodGet, home.ID, "status")
			Expect(registryRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceAuthFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceAuthFailed)))

			ensureErrorAPI := NewPrivateWorkspaceAPI(PrivateWorkspaceAPIOptions{
				Registry: registry,
				Grants:   NewGrantStore(layout.DBPath),
				Subject: func(*http.Request) (WorkspaceSubject, error) {
					return WorkspaceSubject{Subject: "user:admin", Role: GrantRoleAdmin}, nil
				},
				Ensure: func(context.Context, WorkspaceRecord) (WorkspaceStatus, error) {
					return WorkspaceStatus{}, errors.New("ensure failed")
				},
			})
			ensureRec := privateWorkspaceAPIRecorderForWorkspace(ensureErrorAPI, http.MethodPost, home.ID, "ensure")
			Expect(ensureRec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateWorkspaceEnsureFailed, sharederrors.MessageIDForCode(errCodePrivateWorkspaceEnsureFailed)))

			defaultRec := privateWorkspaceAPIRecorderForWorkspace(ensureErrorAPI, http.MethodPatch, home.ID, "status")
			Expect(defaultRec).To(HaveHTTPStatus(http.StatusNotFound))
		})

		ginkgo.It("falls back to a structured encoding error when JSON encoding fails", func() {
			rec := httptest.NewRecorder()

			writeJSON(rec, map[string]any{"bad": make(chan int)})

			Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusInternalServerError, errCodePrivateEncodeResponseFailed, sharederrors.MessageIDForCode(errCodePrivateEncodeResponseFailed)))
		})
	})

	ginkgo.Describe("private handler", func() {
		ginkgo.It("dispatches protected private routes only with the daemon token", func() {
			token := "daemon-token"
			called := map[string]int{}
			handler := NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  token,
				ActorContext: namedHandler("actor", called),
				TokenVerify:  namedHandler("verify", called),
				WorkspaceAPI: namedHandler("workspaces", called),
				Control:      namedHandler("control", called),
			})

			for _, path := range []string{"/__leafwiki/actor-context", "/__leafwiki/token/verify", PrivateWorkspacesPrefix} {
				rec := privateHandlerRecorder(handler, http.MethodGet, path, "")
				Expect(rec).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateUnauthorized, sharederrors.MessageIDForCode(errCodePrivateUnauthorized)), path)
			}
			Expect(privateHandlerRecorder(handler, http.MethodGet, frontd.ControlPlanePrefix+"/status", "")).To(testmatchers.HaveHTTPStructuredError(http.StatusUnauthorized, errCodePrivateUnauthorized, sharederrors.MessageIDForCode(errCodePrivateUnauthorized)))

			Expect(privateHandlerRecorder(handler, http.MethodGet, "/__leafwiki/actor-context", token)).To(HaveHTTPBody("actor"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, "/__leafwiki/token/verify", token)).To(HaveHTTPBody("verify"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, PrivateWorkspacesPrefix, token)).To(HaveHTTPBody("workspaces"))
			Expect(privateHandlerRecorder(handler, http.MethodGet, "/public", "")).To(HaveHTTPBody("control"))
			Expect(called).To(Equal(map[string]int{"actor": 1, "verify": 1, "workspaces": 1, "control": 1}))
		})

		ginkgo.It("returns not found for missing private route handlers", func() {
			handler := NewPrivateHandler(PrivateHandlerOptions{DaemonToken: "daemon-token"})

			for _, path := range []string{
				"/__leafwiki/actor-context",
				"/__leafwiki/token/verify",
				PrivateWorkspacesPrefix,
				frontd.ControlPlanePrefix + "/status",
				"/public",
			} {
				rec := privateHandlerRecorder(handler, http.MethodGet, path, "daemon-token")
				Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), path)
			}
		})

		ginkgo.It("forwards private control-plane requests with path and header normalization", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/base/status"))
				Expect(req.Header.Get(projectdaemon.ControlTokenHeader)).To(BeEmpty())
				Expect(req.Header.Get(projectdaemon.ActorContextHeader)).To(BeEmpty())
				Expect(req.RemoteAddr).To(Equal("10.0.0.2:1234"))
				_, _ = w.Write([]byte(req.URL.Path))
			})
			handler := NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			})
			req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/status", nil)
			req.Header.Set(projectdaemon.ControlTokenHeader, "daemon-token")
			req.Header.Set(projectdaemon.ActorContextHeader, "actor")
			req.Header.Set("X-LeafWiki-Original-Remote-Addr", "10.0.0.2:1234")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("/base/status"),
			))
		})

		ginkgo.It("keeps well-known control-plane paths outside the workspace base path", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/.well-known/oauth-protected-resource"))
				_, _ = w.Write([]byte(req.RemoteAddr))
			})
			req := httptest.NewRequest(http.MethodGet, frontd.ControlPlanePrefix+"/.well-known/oauth-protected-resource", nil)
			req.Header.Set(projectdaemon.ControlTokenHeader, "daemon-token")
			req.RemoteAddr = "127.0.0.1:9999"
			rec := httptest.NewRecorder()

			NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			}).ServeHTTP(rec, req)

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("127.0.0.1:9999"),
			))
		})

		ginkgo.It("forwards the control-plane prefix itself as the root path", func() {
			controlPlane := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				Expect(req.URL.Path).To(Equal("/base/"))
				_, _ = w.Write([]byte(req.URL.Path))
			})
			rec := privateHandlerRecorder(NewPrivateHandler(PrivateHandlerOptions{
				DaemonToken:  "daemon-token",
				BasePath:     "/base",
				ControlPlane: controlPlane,
			}), http.MethodGet, frontd.ControlPlanePrefix, "daemon-token")

			Expect(rec).To(SatisfyAll(
				HaveHTTPStatus(http.StatusOK),
				HaveHTTPBody("/base/"),
			))
		})
	})

	ginkgo.Describe("sqlite helpers", func() {
		ginkgo.It("returns open errors for invalid database paths", func() {
			_, err := openWikidDB(wikidDBPathInsideFile())
			Expect(err).To(MatchError(syscall.ENOTDIR))

			dirPath := filepath.Join(wikidTestTempDir(), "as-directory")
			Expect(os.Mkdir(dirPath, 0o755)).To(Succeed())
			_, err = openWikidDB(dirPath)
			Expect(err).To(MatchError(syscall.EISDIR))
		})

		ginkgo.It("surfaces injected file, chmod, SQL open, and initialization failures", func() {
			restore := captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			closeErr := errors.New("close failed")
			wikidOpenFile = func(string, int, os.FileMode) (wikidCloseFile, error) {
				return closeErrorFile{err: closeErr}, nil
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(closeErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			chmodErr := errors.New("chmod failed")
			wikidChmod = func(string, os.FileMode) error {
				return chmodErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(chmodErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			sqlOpenErr := errors.New("sql open failed")
			wikidSQLOpen = func(string, string) (*sql.DB, error) {
				return nil, sqlOpenErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(sqlOpenErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			initErr := errors.New("init failed")
			wikidInitializeWikidDB = func(*sql.DB) error {
				return initErr
			}
			Expect(openWikidDB(filepath.Join(wikidTestTempDir(), "wikid.db"))).Error().To(MatchError(initErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			pragmaErr := errors.New("pragma failed")
			wikidExecSQLiteWithLockRetry = func(context.Context, wikidSQLiteExecer, string, ...any) (sql.Result, error) {
				return nil, pragmaErr
			}
			Expect(initializeWikidDB(rawSQLiteDB())).To(MatchError(pragmaErr))
		})

		ginkgo.It("rolls back immediate transactions when callbacks fail", func() {
			path := filepath.Join(wikidTestTempDir(), "wikid.db")
			callbackErr := errors.New("callback failed")

			err := withWikidImmediateTx(path, func(context.Context, *sql.Conn) error {
				return callbackErr
			})

			Expect(err).To(MatchError(callbackErr))
		})

		ginkgo.It("surfaces connection, begin, and commit failures from immediate transactions", func() {
			restore := captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			wikidOpenWikidDB = func(string) (*sql.DB, error) {
				db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "closed.db"))
				Expect(err).NotTo(HaveOccurred())
				Expect(db.Close()).To(Succeed())
				return db, nil
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(wikidClosedDatabaseError()))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			beginErr := errors.New("begin failed")
			wikidInitializeWikidDB = func(*sql.DB) error { return nil }
			wikidExecSQLiteWithLockRetry = func(context.Context, wikidSQLiteExecer, string, ...any) (sql.Result, error) {
				return nil, beginErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(beginErr))

			restore()
			err := withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(ctx context.Context, conn *sql.Conn) error {
				_, rollbackErr := conn.ExecContext(ctx, "ROLLBACK")
				Expect(rollbackErr).NotTo(HaveOccurred())
				return nil
			})
			Expect(err).To(matchWikidSQLitePrimaryError(sqlite3.SQLITE_ERROR))

			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			connCloseErr := errors.New("conn close failed")
			wikidCloseConn = func(*sql.Conn) error {
				return connCloseErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(connCloseErr))

			restore()
			restore = captureWikidSQLiteSeams()
			ginkgo.DeferCleanup(restore)
			dbCloseErr := errors.New("db close failed")
			wikidCloseDB = func(*sql.DB) error {
				return dbCloseErr
			}
			Expect(withWikidImmediateTx(filepath.Join(wikidTestTempDir(), "wikid.db"), func(context.Context, *sql.Conn) error {
				return nil
			})).To(MatchError(dbCloseErr))
		})

		ginkgo.It("returns context cancellation while retrying transient SQLite lock errors", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			execer := &lockErrorExecer{err: wikidSQLiteErrorWithCode(5)}

			_, err := execWikidSQLiteWithLockRetry(ctx, execer, "BEGIN IMMEDIATE")

			Expect(err).To(MatchError(context.Canceled))
			Expect(execer.calls).To(Equal(1))
		})

		ginkgo.It("backs off and eventually succeeds after transient SQLite lock errors", func() {
			execer := &lockErrorExecer{err: wikidSQLiteErrorWithCode(6), succeedAfter: 2}

			_, err := execWikidSQLiteWithLockRetry(context.Background(), execer, "BEGIN IMMEDIATE")

			Expect(err).NotTo(HaveOccurred())
			Expect(execer.calls).To(Equal(3))
		})

		ginkgo.It("parses wikid timestamps in UTC and returns parse errors", func() {
			timestamp := time.Date(2026, 6, 27, 14, 30, 0, 123, time.FixedZone("CEST", 2*60*60))

			parsed, err := parseWikidTime(wikidTimeString(timestamp))

			Expect(err).NotTo(HaveOccurred())
			Expect(parsed.Location()).To(Equal(time.UTC))
			Expect(parsed).To(BeTemporally("==", timestamp.UTC()))
			Expect(parseWikidTime("not-a-time")).Error().To(matchWikidTimeParseError())
		})
	})
})

func newWikidEdgeLayout() Layout {
	ginkgo.GinkgoHelper()
	return GlobalLayout(filepath.Join(wikidTestTempDir(), ".leafwiki"))
}

func wikidDBPathInsideFile() string {
	ginkgo.GinkgoHelper()
	parentFile := filepath.Join(wikidTestTempDir(), "not-a-dir")
	Expect(os.WriteFile(parentFile, []byte("x"), 0o644)).To(Succeed())
	return filepath.Join(parentFile, "wikid.db")
}

func rawSQLiteDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "raw.db"))
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	return db
}

func rawSQLiteDBAt(path string) *sql.DB {
	ginkgo.GinkgoHelper()
	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	db, err := sql.Open("sqlite", path)
	Expect(err).NotTo(HaveOccurred())
	return db
}

func rawGrantDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db := rawSQLiteDB()
	Expect(execRawSQL(db, `CREATE TABLE workspace_grants (subject TEXT, workspace_id TEXT, role TEXT)`)).To(Succeed())
	return db
}

func rawRegistryDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db := rawSQLiteDB()
	Expect(execRawSQL(db, `CREATE TABLE workspaces (
		id TEXT,
		display_name TEXT,
		data_dir TEXT,
		root_dir TEXT,
		markdown_link_root_prefix TEXT,
		created_at TEXT,
		updated_at TEXT
	)`)).To(Succeed())
	return db
}

func closedSQLiteDB() *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := sql.Open("sqlite", filepath.Join(wikidTestTempDir(), "closed.db"))
	Expect(err).NotTo(HaveOccurred())
	Expect(db.Close()).To(Succeed())
	return db
}

func wikidClosedDatabaseError() error {
	ginkgo.GinkgoHelper()
	db := closedSQLiteDB()
	_, err := db.Conn(context.Background())
	return err
}

func mustOpenInitializedWikidDB(path string) *sql.DB {
	ginkgo.GinkgoHelper()
	db, err := openWikidDB(path)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(db.Close()).To(Succeed())
	})
	return db
}

func execRawSQL(db *sql.DB, query string) error {
	ginkgo.GinkgoHelper()
	_, err := db.Exec(query)
	return err
}

func validWorkspaceSubject(*http.Request) (WorkspaceSubject, error) {
	return WorkspaceSubject{Subject: "user:1"}, nil
}

func privateWorkspaceAPIStatus(api http.Handler, method string, path string) int {
	ginkgo.GinkgoHelper()
	return privateWorkspaceAPIRecorder(api, method, path).Code
}

func privateWorkspaceAPIRecorder(api http.Handler, method string, path string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func privateWorkspaceAPIRecorderForWorkspace(api http.Handler, method string, id workspaceid.WorkspaceID, action string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, PrivateWorkspacesPrefix+"/"+id.URLPathSegment()+"/"+action, nil)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func privateHandlerRecorder(handler http.Handler, method string, path string, token string) *httptest.ResponseRecorder {
	ginkgo.GinkgoHelper()
	req := httptest.NewRequest(method, path, nil)
	if strings.TrimSpace(token) != "" {
		req.Header.Set(projectdaemon.ControlTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func namedHandler(name string, called map[string]int) http.Handler {
	ginkgo.GinkgoHelper()
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called[name]++
		_, _ = w.Write([]byte(name))
	})
}

type lockErrorExecer struct {
	err          error
	succeedAfter int
	calls        int
}

func (e *lockErrorExecer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	e.calls++
	if e.succeedAfter > 0 && e.calls > e.succeedAfter {
		return nil, nil
	}
	return nil, e.err
}

func wikidSQLiteErrorWithCode(code int) error {
	e := &sqlite.Error{}
	v := reflect.ValueOf(e).Elem().FieldByName("code")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().SetInt(int64(code))
	return e
}

type workspaceOrderSnapshot struct {
	FirstID           workspaceid.WorkspaceID
	SecondDisplayName string
	ThirdDisplayName  string
}

func haveHomeWorkspaceFirstAndEqualFoldTie() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveLen(3),
		WithTransform(func(workspaces []WorkspaceRecord) workspaceOrderSnapshot {
			if len(workspaces) < 3 {
				return workspaceOrderSnapshot{}
			}
			return workspaceOrderSnapshot{
				FirstID:           workspaces[0].ID,
				SecondDisplayName: workspaces[1].DisplayName,
				ThirdDisplayName:  workspaces[2].DisplayName,
			}
		}, SatisfyAll(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"FirstID": Equal(HomeWorkspaceID),
			}),
			WithTransform(func(snapshot workspaceOrderSnapshot) bool {
				return strings.EqualFold(snapshot.SecondDisplayName, snapshot.ThirdDisplayName)
			}, BeTrue()),
		)),
	)
}

func matchWikidSQLitePrimaryError(code int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(err error) int {
		var sqliteErr *sqlite.Error
		if !errors.As(err, &sqliteErr) {
			return -1
		}
		return sqliteErr.Code() & 0xFF
	}, Equal(code))
}

func matchWikidTimeParseError() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return Satisfy(func(err error) bool {
		var parseErr *time.ParseError
		return errors.As(err, &parseErr)
	})
}

type errRows struct {
	err error
}

func (r errRows) Next() bool {
	return false
}

func (r errRows) Scan(...any) error {
	return nil
}

func (r errRows) Err() error {
	return r.err
}

type closeErrorFile struct {
	err error
}

func (f closeErrorFile) Close() error {
	return f.err
}

func captureWikidAuthSeams() func() {
	ginkgo.GinkgoHelper()
	remove := wikidRemove
	mkdirAll := wikidMkdirAll
	newUserStore := wikidNewUserStore
	newSessionStore := wikidNewSessionStore
	newAPIKeyStore := wikidNewAPIKeyStore
	return func() {
		wikidRemove = remove
		wikidMkdirAll = mkdirAll
		wikidNewUserStore = newUserStore
		wikidNewSessionStore = newSessionStore
		wikidNewAPIKeyStore = newAPIKeyStore
	}
}

func captureWikidPathSeams() func() {
	ginkgo.GinkgoHelper()
	filepathAbs := wikidFilepathAbs
	evalSymlinks := wikidEvalSymlinks
	return func() {
		wikidFilepathAbs = filepathAbs
		wikidEvalSymlinks = evalSymlinks
	}
}

func captureWikidSQLiteSeams() func() {
	ginkgo.GinkgoHelper()
	mkdirAll := wikidMkdirAll
	openFile := wikidOpenFile
	chmod := wikidChmod
	sqlOpen := wikidSQLOpen
	initialize := wikidInitializeWikidDB
	openDB := wikidOpenWikidDB
	closeDB := wikidCloseDB
	closeConn := wikidCloseConn
	execWithRetry := wikidExecSQLiteWithLockRetry
	return func() {
		wikidMkdirAll = mkdirAll
		wikidOpenFile = openFile
		wikidChmod = chmod
		wikidSQLOpen = sqlOpen
		wikidInitializeWikidDB = initialize
		wikidOpenWikidDB = openDB
		wikidCloseDB = closeDB
		wikidCloseConn = closeConn
		wikidExecSQLiteWithLockRetry = execWithRetry
	}
}
