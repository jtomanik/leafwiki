package identity

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("semantic identity types", func() {
	It("normalizes user IDs for metadata and actor contracts", func() {
		userID := UserIDFromString(" user-1 ")

		Expect(userID).To(Equal(NewUserIDUnchecked(" user-1 ")))
		Expect(userID.String()).To(Equal(" user-1 "))
		Expect(userID.HashPayload()).To(Equal(" user-1 "))
		Expect(userID.MetadataValue()).To(Equal("user-1"))
		Expect(userID.ActorID()).To(Equal("user-1"))
	})

	It("preserves revision IDs for revision and commit contracts", func() {
		revisionID := RevisionIDFromString("rev-1")

		Expect(revisionID).To(Equal(NewRevisionIDUnchecked("rev-1")))
		Expect(revisionID.String()).To(Equal("rev-1"))
		Expect(revisionID.CommitID()).To(Equal("rev-1"))
	})

	It("preserves commit hash string values", func() {
		commitHash := CommitHashFromString("abc123")

		Expect(commitHash).To(Equal(NewCommitHashUnchecked("abc123")))
		Expect(commitHash.String()).To(Equal("abc123"))
	})
})
