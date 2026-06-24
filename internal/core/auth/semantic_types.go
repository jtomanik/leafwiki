package auth

import "github.com/perber/wiki/internal/core/identity"

type UserID = identity.UserID

func NewUserIDUnchecked(raw string) UserID {
	return identity.NewUserIDUnchecked(raw)
}

func UserIDFromString[T ~string](raw T) UserID {
	return identity.UserIDFromString(raw)
}

type APIKeyID string

func (id APIKeyID) String() string {
	return string(id)
}

func NewAPIKeyIDUnchecked(raw string) APIKeyID {
	return APIKeyID(raw)
}

func APIKeyIDFromString[T ~string](raw T) APIKeyID {
	return NewAPIKeyIDUnchecked(string(raw))
}

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

func NewSessionIDUnchecked(raw string) SessionID {
	return SessionID(raw)
}

func SessionIDFromString[T ~string](raw T) SessionID {
	return NewSessionIDUnchecked(string(raw))
}
