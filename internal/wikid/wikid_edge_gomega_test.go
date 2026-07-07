package wikid

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("wikid auth storage and document validation", func() {
	ginkgo.Describe("auth storage", func() {
		ginkgo.It("joins legacy cleanup errors and propagates auth directory setup failures", ginkgo.Label("integration"), func() {
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

		ginkgo.It("treats nil auth store collections as already closed", ginkgo.Label("unit"), func() {
			var stores *AuthStores
			Expect(stores.Close()).To(Succeed())
		})

		ginkgo.It("propagates auth store constructor failures after directory setup succeeds", ginkgo.Label("integration"), func() {
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

	ginkgo.Describe("document validation", ginkgo.Label("unit"), func() {
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
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: mustDecodeWorkspaceID("bad/id"), DataDir: "/tmp/data", RootDir: "/tmp/root"})).To(testmatchers.HaveStructuredError(workspaceid.ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(workspaceid.ErrCodeWorkspaceIDInvalid)))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, RootDir: "/tmp/root"})).To(MatchError(ErrWorkspaceDataDirRequired))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data"})).To(MatchError(ErrWorkspaceRootDirRequired))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data", RootDir: "/tmp/root", MarkdownLinkRootPrefix: "docs/"})).To(MatchError(ErrWorkspaceMarkdownLinkRootPrefixNotNormalized))
			Expect(validateWorkspaceRecord(WorkspaceRecord{ID: HomeWorkspaceID, DataDir: "/tmp/data", RootDir: "/tmp/root", MarkdownLinkRootPrefix: "../docs"})).To(MatchError(markdownlinks.ErrMarkdownLinkRootPrefixTraversal))
		})

		ginkgo.It("reports grant document validation failures", func() {
			Expect((GrantDocument{}).Validate()).To(MatchError(ErrGrantSchemaVersion))
			Expect(validateGrant(Grant{WorkspaceID: HomeWorkspaceID, Role: GrantRoleViewer})).To(MatchError(ErrGrantSubjectRequired))
			Expect(validateGrant(Grant{Subject: "user:1", WorkspaceID: mustDecodeWorkspaceID("bad/id"), Role: GrantRoleViewer})).To(matchWikidWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))
			Expect(validateGrant(Grant{Subject: "user:1", WorkspaceID: HomeWorkspaceID, Role: mustDecodeGrantRole("owner")})).To(MatchError(ErrUnknownGrantRole))
			Expect(CapabilitiesForRole(mustDecodeGrantRole("owner"))).Error().To(MatchError(ErrUnknownGrantRole))
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
})
