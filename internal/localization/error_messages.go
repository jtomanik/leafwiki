package localization

// derivedErrorMessages covers MessageIDForCode-derived backend error IDs.
var derivedErrorMessages = combineLocalizationMessages(
	derivedErrorMessagesCore,
	derivedErrorMessagesPages,
	derivedErrorMessagesWorkspace,
)
