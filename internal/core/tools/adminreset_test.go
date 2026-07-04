package tools

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/perber/wiki/internal/core/auth"
)

const (
	resetAdminUsername             = auth.DefaultAdminUsername
	resetAdminEmail                = auth.DefaultAdminEmail
	resetAdminCloseStoreLogMessage = "could not close store"
)

var errResetAdminCloseStore = errors.New("auth store close failed")

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

func matchAdminStoreOpenSQLiteError() types.GomegaMatcher {
	return WithTransform(sqliteErrorCodeFor, Equal(sqliteErrorCode(sqlite3.SQLITE_CANTOPEN)))
}

type sqliteErrorCode int

const sqliteErrorAbsent sqliteErrorCode = -1

func sqliteErrorCodeFor(err error) sqliteErrorCode {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return sqliteErrorAbsent
	}
	return sqliteErrorCode(sqliteErr.Code())
}

type capturedLogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type recordingSlogHandler struct {
	records *[]capturedLogRecord
}

func newRecordingLogger() (*slog.Logger, *[]capturedLogRecord) {
	records := []capturedLogRecord{}
	return slog.New(recordingSlogHandler{records: &records}), &records
}

func (handler recordingSlogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (handler recordingSlogHandler) Handle(_ context.Context, record slog.Record) error {
	attrs := map[string]any{}
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	*handler.records = append(*handler.records, capturedLogRecord{
		Level:   record.Level,
		Message: record.Message,
		Attrs:   attrs,
	})
	return nil
}

func (handler recordingSlogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return handler
}

func (handler recordingSlogHandler) WithGroup(string) slog.Handler {
	return handler
}

func matchAuthStoreCloseErrorLog(closeErr error) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Level":   Equal(slog.LevelError),
		"Message": Equal(resetAdminCloseStoreLogMessage),
		"Attrs":   HaveKeyWithValue("error", closeErr),
	})
}

var _ = ginkgo.Describe("admin password reset", func() {
	ginkgo.It("resets the existing admin password and persists the new credentials", ginkgo.Label("integration"), func() {
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

	ginkgo.It("creates a login-ready default admin when no admin exists", ginkgo.Label("integration"), func() {
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

	ginkgo.It("invalidates the previous admin password and preserves regular users", ginkgo.Label("integration"), func() {
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

	ginkgo.It("creates a complete default admin user when none exists", ginkgo.Label("integration"), func() {
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

	ginkgo.It("returns an error and nil user when the auth store cannot open", ginkgo.Label("integration"), func() {
		storageFile := filepath.Join(tempAdminResetStorageDir(), "not-a-directory")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o644)).To(Succeed())

		adminUser, err := ResetAdminPassword(storageFile)

		Expect(err).To(matchAdminStoreOpenSQLiteError())
		Expect(adminUser).To(BeNil())
	})

	ginkgo.It("logs auth store close errors", ginkgo.Label("unit"), func() {
		logger, records := newRecordingLogger()

		logUserStoreClose(logger, closeErrorStore{err: errResetAdminCloseStore})

		Expect(*records).To(ConsistOf(matchAuthStoreCloseErrorLog(errResetAdminCloseStore)))
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
