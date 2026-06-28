package tools

import (
	"log/slog"

	"github.com/perber/wiki/internal/core/auth"
)

func ResetAdminPassword(storageDir string) (*auth.User, error) {
	store, err := auth.NewUserStore(storageDir)
	if err != nil {
		return nil, err
	}
	defer logUserStoreClose(slog.Default(), store)

	userService := auth.NewUserService(store)
	return userService.ResetAdminUserPassword()
}

func logUserStoreClose(log *slog.Logger, store interface{ Close() error }) {
	if err := store.Close(); err != nil {
		log.Error("could not close store", "error", err)
	}
}
