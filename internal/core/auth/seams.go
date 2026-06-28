package auth

import (
	"crypto/rand"
	"database/sql"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/shared/sqliteutil"
	"golang.org/x/crypto/bcrypt"
)

var (
	authRandRead               = rand.Read
	authSQLOpen                = sql.Open
	authGenerateUniqueID       = shared.GenerateUniqueID
	authGenerateRandomPassword = shared.GenerateRandomPassword
	authGeneratePasswordHash   = bcrypt.GenerateFromPassword
	authJWTParse               = jwt.Parse
	authSignJWT                = func(token *jwt.Token, key []byte) (string, error) {
		return token.SignedString(key)
	}
	authSessionCleanupInterval = time.Hour
	authAPIKeyRetryMaxAttempts = 100
	authAPIKeyRetryDelay       = 20 * time.Millisecond
	authIsSQLiteTransientLock  = sqliteutil.IsSQLiteTransientLockError
	authCloseRows              = func(rows interface{ Close() error }) error {
		return rows.Close()
	}

	authAPIKeyStoreCreateAPIKey      = (*APIKeyStore).CreateAPIKey
	authAPIKeyStoreGetAPIKeyByID     = (*APIKeyStore).GetAPIKeyByID
	authAPIKeyStoreListActiveAPIKeys = (*APIKeyStore).ListActiveAPIKeys
	authAPIKeyStoreMarkAPIKeyUsed    = (*APIKeyStore).MarkAPIKeyUsed
	authAPIKeyStoreRevokeAPIKey      = (*APIKeyStore).RevokeAPIKey

	authSessionStoreCreateSession            = (*SessionStore).CreateSession
	authSessionStoreIsActive                 = (*SessionStore).IsActive
	authSessionStoreRevokeSession            = (*SessionStore).RevokeSession
	authSessionStoreRevokeAllSessionsForUser = (*SessionStore).RevokeAllSessionsForUser
	authUserStoreGetUserByID                 = (*UserStore).GetUserByID
	authUserStoreGetUserByUsername           = (*UserStore).GetUserByUsername
	authUserStoreGetUserByEmail              = (*UserStore).GetUserByEmail
	authUserStoreGetAdminUser                = (*UserStore).GetAdminUser
	authUserStoreGetAllUsers                 = (*UserStore).GetAllUsers
	authUserStoreCreateUser                  = (*UserStore).CreateUser
	authUserStoreUpdateUser                  = (*UserStore).UpdateUser
	authUserStoreUpdatePassword              = (*UserStore).UpdatePassword
	authUserStoreDeleteUser                  = (*UserStore).DeleteUser
	authUserStoreCountAdminUsers             = (*UserStore).CountAdminUsers
)
