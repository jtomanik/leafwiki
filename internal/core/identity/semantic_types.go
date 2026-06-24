package identity

import "strings"

type UserID string

func NewUserIDUnchecked(raw string) UserID {
	return UserID(raw)
}

func UserIDFromString[T ~string](raw T) UserID {
	return NewUserIDUnchecked(string(raw))
}

func (id UserID) String() string {
	return string(id)
}

func (id UserID) HashPayload() string {
	return string(id)
}

func (id UserID) MetadataValue() string {
	return strings.TrimSpace(string(id))
}

func (id UserID) ActorID() string {
	return strings.TrimSpace(string(id))
}

type RevisionID string

func NewRevisionIDUnchecked(raw string) RevisionID {
	return RevisionID(raw)
}

func RevisionIDFromString[T ~string](raw T) RevisionID {
	return NewRevisionIDUnchecked(string(raw))
}

func (id RevisionID) String() string {
	return string(id)
}

func (id RevisionID) CommitID() string {
	return string(id)
}

type CommitHash string

func NewCommitHashUnchecked(raw string) CommitHash {
	return CommitHash(raw)
}

func CommitHashFromString[T ~string](raw T) CommitHash {
	return NewCommitHashUnchecked(string(raw))
}

func (hash CommitHash) String() string {
	return string(hash)
}
