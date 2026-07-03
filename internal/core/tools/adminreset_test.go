package tools

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	"github.com/perber/wiki/internal/core/auth"
)

const (
	resetAdminUsername              = auth.DefaultAdminUsername
	resetAdminEmail                 = auth.DefaultAdminEmail
	resetAdminCloseStoreLogMessage  = "could not close store"
	resetAdminCloseStoreFixtureText = "close failed"
)

func matchPasswordResetUser(username string) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Username": Equal(username),
		"Password": Not(BeEmpty()),
	}))
}

func matchDefaultAdminUser() types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Username": Equal(resetAdminUsername),
		"Email":    Equal(resetAdminEmail),
		"Role":     Equal(auth.RoleAdmin),
		"Password": Not(BeEmpty()),
	}))
}

func matchPersistedAdminUser() types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Email": Equal(resetAdminEmail),
		"Role":  Equal(auth.RoleAdmin),
	}))
}

var _ = ginkgo.Describe("admin password reset", func() {
	ginkgo.It("resets the existing admin password and persists the new credentials", func() {
		storageDir := tempAdminResetStorageDir()
		store := openUserStore(storageDir)
		userService := auth.NewUserService(store)
		_, err := userService.CreateUser("admin", "admin@example.com", "oldpassword", auth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser).To(matchPasswordResetUser(resetAdminUsername))

		store = openUserStore(storageDir)
		ginkgo.DeferCleanup(closeUserStore, store)

		userService = auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword(resetAdminUsername, adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("creates a login-ready default admin when no admin exists", func() {
		storageDir := tempAdminResetStorageDir()

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser).To(matchDefaultAdminUser())

		store := openUserStore(storageDir)
		ginkgo.DeferCleanup(closeUserStore, store)

		userService := auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword(resetAdminUsername, adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("invalidates the previous admin password and preserves regular users", func() {
		storageDir := tempAdminResetStorageDir()
		store := openUserStore(storageDir)
		userService := auth.NewUserService(store)
		_, err := userService.CreateUser("admin", "admin@example.com", "oldpassword", auth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		_, err = userService.CreateUser("editor", "editor@example.com", "editorpassword", auth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		store = openUserStore(storageDir)
		ginkgo.DeferCleanup(closeUserStore, store)

		userService = auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword(resetAdminUsername, "oldpassword")
		Expect(err).To(MatchError(auth.ErrUserInvalidCredentials))
		_, err = userService.GetUserByEmailOrUsernameAndPassword(resetAdminUsername, adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
		editor, err := userService.GetUserByEmailOrUsernameAndPassword("editor", "editorpassword")
		Expect(err).NotTo(HaveOccurred())
		Expect(editor.Role).To(Equal(auth.RoleEditor))
	})

	ginkgo.It("creates a complete default admin user when none exists", func() {
		storageDir := tempAdminResetStorageDir()

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser).To(matchDefaultAdminUser())

		store := openUserStore(storageDir)
		ginkgo.DeferCleanup(closeUserStore, store)

		userService := auth.NewUserService(store)
		persisted, err := userService.GetUserByEmailOrUsernameAndPassword(resetAdminUsername, adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
		Expect(persisted).To(matchPersistedAdminUser())
	})

	ginkgo.It("returns an error and nil user when the auth store cannot open", func() {
		storageFile := filepath.Join(tempAdminResetStorageDir(), "not-a-directory")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o644)).To(Succeed())

		adminUser, err := ResetAdminPassword(storageFile)

		Expect(err).To(HaveOccurred())
		Expect(adminUser).To(BeNil())
	})

	ginkgo.It("logs auth store close errors", func() {
		var logOutput bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logOutput, nil))

		logUserStoreClose(logger, closeErrorStore{err: errors.New(resetAdminCloseStoreFixtureText)})

		Expect(logOutput.String()).To(SatisfyAll(
			ContainSubstring(resetAdminCloseStoreLogMessage),
			ContainSubstring(resetAdminCloseStoreFixtureText),
		))
	})
})

func tempAdminResetStorageDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-admin-reset-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func openUserStore(storageDir string) *auth.UserStore {
	ginkgo.GinkgoHelper()

	store, err := auth.NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	return store
}

func closeUserStore(store *auth.UserStore) {
	ginkgo.GinkgoHelper()

	Expect(store.Close()).To(Succeed())
}

type closeErrorStore struct {
	err error
}

func (s closeErrorStore) Close() error {
	return s.err
}
