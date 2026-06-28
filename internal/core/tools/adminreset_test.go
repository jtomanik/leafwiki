package tools

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/auth"
)

var _ = ginkgo.Describe("admin reset", func() {
	ginkgo.It("TestResetAdminPassword", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
		store := openUserStore(storageDir)
		userService := auth.NewUserService(store)
		_, err := userService.CreateUser("admin", "admin@example.com", "oldpassword", auth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		Expect(store.Close()).To(Succeed())

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser.Username).To(Equal("admin"))
		Expect(adminUser.Password).NotTo(BeEmpty())

		store = openUserStore(storageDir)
		defer closeUserStore(store)

		userService = auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("TestResetAdminPassword_NoAdmin", func() {
		storageDir := ginkgo.GinkgoT().TempDir()

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser.Username).To(Equal("admin"))
		Expect(adminUser.Email).To(Equal("admin@localhost"))
		Expect(adminUser.Password).NotTo(BeEmpty())

		store := openUserStore(storageDir)
		defer closeUserStore(store)

		userService := auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("invalidates the previous admin password and preserves regular users", func() {
		storageDir := ginkgo.GinkgoT().TempDir()
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
		defer closeUserStore(store)

		userService = auth.NewUserService(store)
		_, err = userService.GetUserByEmailOrUsernameAndPassword("admin", "oldpassword")
		Expect(err).To(MatchError(auth.ErrUserInvalidCredentials))
		_, err = userService.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
		editor, err := userService.GetUserByEmailOrUsernameAndPassword("editor", "editorpassword")
		Expect(err).NotTo(HaveOccurred())
		Expect(editor.Role).To(Equal(auth.RoleEditor))
	})

	ginkgo.It("creates a complete default admin user when none exists", func() {
		storageDir := ginkgo.GinkgoT().TempDir()

		adminUser, err := ResetAdminPassword(storageDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(adminUser.Username).To(Equal("admin"))
		Expect(adminUser.Email).To(Equal("admin@localhost"))
		Expect(adminUser.Role).To(Equal(auth.RoleAdmin))
		Expect(adminUser.Password).NotTo(BeEmpty())

		store := openUserStore(storageDir)
		defer closeUserStore(store)

		userService := auth.NewUserService(store)
		persisted, err := userService.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		Expect(err).NotTo(HaveOccurred())
		Expect(persisted.Email).To(Equal("admin@localhost"))
		Expect(persisted.Role).To(Equal(auth.RoleAdmin))
	})

	ginkgo.It("returns an error and nil user when the auth store cannot open", func() {
		storageFile := filepath.Join(ginkgo.GinkgoT().TempDir(), "not-a-directory")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o644)).To(Succeed())

		adminUser, err := ResetAdminPassword(storageFile)

		Expect(err).To(HaveOccurred())
		Expect(adminUser).To(BeNil())
	})

	ginkgo.It("logs auth store close errors", func() {
		var logOutput bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logOutput, nil))

		logUserStoreClose(logger, closeErrorStore{err: errors.New("close failed")})

		Expect(logOutput.String()).To(ContainSubstring("could not close store"))
		Expect(logOutput.String()).To(ContainSubstring("close failed"))
	})
})

func openUserStore(storageDir string) *auth.UserStore {
	store, err := auth.NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	return store
}

func closeUserStore(store *auth.UserStore) {
	Expect(store.Close()).To(Succeed())
}

type closeErrorStore struct {
	err error
}

func (s closeErrorStore) Close() error {
	return s.err
}
