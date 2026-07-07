package localization

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

const (
	testCatalogDefault  = "Catalog default"
	testRegistryDefault = "Registry default"
)

type defaultMismatchFormatObservation struct {
	ID              CatalogMessageID
	CatalogDefault  string
	RegistryDefault string
	Err             error
}

type missingMessagesFormatObservation struct {
	IDs []CatalogMessageID
	Err error
}

type templateMismatchFormatObservation struct {
	ID  CatalogMessageID
	Err error
}

type directCatalogLookupObservation struct {
	MessageID CatalogMessageID
	Renderer  catalogRendererState
	Message   catalogMessagePresence
}

var _ = Describe("localization catalog error contracts", Label("unit"), func() {
	It("preserves default mismatch identity when the error is formatted", func() {
		messageID := CatalogMessageID(MessageIDCLIHelpUsage)
		err := &CommittedCatalogDefaultMismatchError{
			ID:              messageID,
			CatalogDefault:  testCatalogDefault,
			RegistryDefault: testRegistryDefault,
		}

		Expect(observeDefaultMismatchFormat(err)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":              Equal(messageID),
			"CatalogDefault":  Equal(testCatalogDefault),
			"RegistryDefault": Equal(testRegistryDefault),
			"Err":             MatchError(ErrCommittedCatalogDefaultMismatch),
		}))
	})

	It("preserves missing-message identity when the error is formatted", func() {
		messageID := CatalogMessageID(MessageIDCLIHelpUsage)
		err := &CommittedCatalogMissingMessagesError{IDs: []CatalogMessageID{messageID}}

		Expect(observeMissingMessagesFormat(err)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"IDs": Equal([]CatalogMessageID{messageID}),
			"Err": MatchError(ErrCommittedCatalogMissingMessage),
		}))
	})

	It("preserves template mismatch identity when the error is formatted", func() {
		messageID := CatalogMessageID(MessageIDCLIHelpUsage)
		err := &TemplateDataMismatchError{MessageID: messageID}

		Expect(observeTemplateMismatchFormat(err)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ID":  Equal(messageID),
			"Err": MatchError(ErrLocalizationTemplateDataMismatch),
		}))
	})

	It("reports catalog IDs unavailable when the renderer is nil", func() {
		var renderer *Renderer
		messageID := CatalogMessageID(MessageIDCLIHelpUsage)

		Expect(observeDirectCatalogLookup(renderer, messageID)).To(Equal(directCatalogLookupObservation{
			MessageID: messageID,
			Renderer:  catalogRendererUnavailable,
			Message:   catalogMessageAbsent,
		}))
	})
})

func observeDefaultMismatchFormat(err *CommittedCatalogDefaultMismatchError) defaultMismatchFormatObservation {
	return defaultMismatchFormatObservation{
		ID:              err.ID,
		CatalogDefault:  err.CatalogDefault,
		RegistryDefault: err.RegistryDefault,
		Err:             fmt.Errorf("%w", err),
	}
}

func observeMissingMessagesFormat(err *CommittedCatalogMissingMessagesError) missingMessagesFormatObservation {
	return missingMessagesFormatObservation{
		IDs: err.MessageIDs(),
		Err: fmt.Errorf("%w", err),
	}
}

func observeTemplateMismatchFormat(err *TemplateDataMismatchError) templateMismatchFormatObservation {
	return templateMismatchFormatObservation{
		ID:  err.MessageID,
		Err: fmt.Errorf("%w", err),
	}
}

func observeDirectCatalogLookup(renderer *Renderer, messageID CatalogMessageID) directCatalogLookupObservation {
	message := catalogMessageAbsent
	if renderer.hasCatalogID(fmt.Sprint(messageID)) {
		message = catalogMessageAvailable
	}
	rendererState := catalogRendererReady
	if renderer == nil {
		rendererState = catalogRendererUnavailable
	}
	return directCatalogLookupObservation{
		MessageID: messageID,
		Renderer:  rendererState,
		Message:   message,
	}
}
