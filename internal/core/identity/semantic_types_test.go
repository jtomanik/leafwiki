package identity

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("semantic identity types", Label("unit"), func() {
	It("normalizes user IDs for metadata and actor contracts", func() {
		const expectedUserID UserID = " user-1 "
		userID := UserIDFromString(" user-1 ")

		Expect(userID).To(Equal(expectedUserID))
		Expect(userID.HashPayload()).To(Equal(" user-1 "))
		Expect(userID.MetadataValue()).To(Equal("user-1"))
		Expect(userID.ActorID()).To(Equal("user-1"))
	})

	It("preserves revision IDs for revision and commit contracts", func() {
		const expectedRevisionID RevisionID = "rev-1"
		revisionID := RevisionIDFromString("rev-1")

		Expect(revisionID).To(Equal(expectedRevisionID))
		Expect(revisionID.CommitID()).To(Equal("rev-1"))
	})

	It("preserves commit hash string values", func() {
		const expectedCommitHash CommitHash = "abc123"
		commitHash := CommitHashFromString("abc123")

		Expect(commitHash).To(Equal(expectedCommitHash))
	})
})
