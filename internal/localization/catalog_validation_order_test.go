package localization

import (
	"errors"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	missingCatalogAlphaID CatalogMessageID = "test.catalog.alpha"
	missingCatalogZuluID  CatalogMessageID = "test.catalog.zulu"
)

var _ = Describe("committed catalog validation", Label("unit"), func() {
	It("reports missing catalog obligations in message ID order", func() {
		messages := append([]*i18n.Message(nil), registryMessages...)
		messages = append(messages,
			catalogValidationMessage(missingCatalogZuluID),
			catalogValidationMessage(missingCatalogAlphaID),
		)
		replaceRegistryMessages(messages)

		err := ValidateCommittedCatalog()

		Expect(err).To(SatisfyAll(
			MatchError(ErrCommittedCatalogMissingMessage),
			WithTransform(missingCatalogMessageIDs, Equal([]CatalogMessageID{
				missingCatalogAlphaID,
				missingCatalogZuluID,
			})),
		))
	})
})

func catalogValidationMessage(id CatalogMessageID) *i18n.Message {
	return &i18n.Message{
		ID:          string(id),
		Description: "Synthetic catalog validation obligation.",
		Other:       "Synthetic catalog validation message.",
	}
}

func missingCatalogMessageIDs(err error) []CatalogMessageID {
	var catalogErr *CommittedCatalogMissingMessagesError
	if !errors.As(err, &catalogErr) {
		return nil
	}
	return catalogErr.MessageIDs()
}
