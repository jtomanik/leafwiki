package semanticcases

import (
	"fmt"
	"net/http"
	"os"

	policytree "github.com/perber/wiki/internal/core/tree"
)

type CommitHash string

func (hash CommitHash) String() string {
	return string(hash)
}

type revisionStorePolicyGap interface {
	ChangedMarkdownPaths(commitHash string) []string // want "semantic-looking parameter commitHash uses string in internal function ChangedMarkdownPaths; use CommitHash or accept a DTO boundary value"
	RestoreWorkspace(commitHash string)              // want "semantic-looking parameter commitHash uses string in internal function RestoreWorkspace; use CommitHash or accept a DTO boundary value"
	RestoreDocument(string, string)                  // want "semantic-looking parameter commitHash uses string in internal function RestoreDocument; use CommitHash or accept a DTO boundary value"
}

type gitRevisionStorePolicyGap struct{}

func (gitRevisionStorePolicyGap) GetCommit(commitHash string) { // want "semantic-looking parameter commitHash uses string in internal function GetCommit; use CommitHash or accept a DTO boundary value"
}

func forbiddenCommitHashStringCall(store revisionStorePolicyGap, commitHash CommitHash) {
	store.ChangedMarkdownPaths(commitHash.String()) // want "semantic value CommitHash converted to string before internal call ChangedMarkdownPaths; make the callee accept CommitHash"
}

type SessionID string

func (id SessionID) String() string {
	return string(id)
}

type heartbeatPolicyGap struct {
	SessionID string // want "semantic-looking field SessionID uses string in domain/service type heartbeatPolicyGap; use SessionID or mark the type as a DTO boundary"
}

type sessionRegistryPolicyGap struct{}

func (sessionRegistryPolicyGap) Bind(sessionID string) { // want "semantic-looking parameter sessionID uses string in internal function Bind; use SessionID or accept a DTO boundary value"
}

func (sessionRegistryPolicyGap) Unbind(sessionID string) { // want "semantic-looking parameter sessionID uses string in internal function Unbind; use SessionID or accept a DTO boundary value"
}

func (sessionRegistryPolicyGap) Heartbeat(sessionID string) { // want "semantic-looking parameter sessionID uses string in internal function Heartbeat; use SessionID or accept a DTO boundary value"
}

func (sessionRegistryPolicyGap) Release(sessionID string) { // want "semantic-looking parameter sessionID uses string in internal function Release; use SessionID or accept a DTO boundary value"
}

func (sessionRegistryPolicyGap) Record(sessionID string) { // want "semantic-looking parameter sessionID uses string in internal function Record; use SessionID or accept a DTO boundary value"
}

func forbiddenSessionIDStringCall(registry sessionRegistryPolicyGap, sessionID SessionID) {
	registry.Bind(sessionID.String()) // want "semantic value SessionID converted to string before internal call Bind; make the callee accept SessionID"
}

type linkMarkerPolicyGap struct{}

func (linkMarkerPolicyGap) MarkLinksBrokenForPath(toPath string) { // want "semantic-looking parameter toPath uses string in internal function MarkLinksBrokenForPath; use RoutePath or accept a DTO boundary value"
}

func (linkMarkerPolicyGap) MarkLinkTransition(pagePath string, oldPath string, targetPath string) { // want "semantic-looking parameter pagePath uses string in internal function MarkLinkTransition; use RoutePath or accept a DTO boundary value" "semantic-looking parameter oldPath uses string in internal function MarkLinkTransition; use RoutePath or accept a DTO boundary value" "semantic-looking parameter targetPath uses string in internal function MarkLinkTransition; use RoutePath or accept a DTO boundary value"
}

func (linkMarkerPolicyGap) ResolveCurrentPath(currentPath string) { // want "semantic-looking parameter currentPath uses string in internal function ResolveCurrentPath; use RoutePath or accept a DTO boundary value"
}

type searchInputPolicyGap struct {
	Offset int // want "semantic-looking field Offset uses int in domain/service type searchInputPolicyGap; introduce a typed value after parsing"
	Limit  int // want "semantic-looking field Limit uses int in domain/service type searchInputPolicyGap; introduce a typed value after parsing"
}

func (PageService) Search(query string, offset int, limit int) { // want "semantic-looking parameter offset uses int in internal function Search; introduce a typed value after parsing" "semantic-looking parameter limit uses int in internal function Search; introduce a typed value after parsing"
}

func (PageService) subtreeNode(depth int) { // want "semantic-looking parameter depth uses int in internal function subtreeNode; introduce a typed value after parsing"
}

func (PageService) SaveAssetForPage(maxBytes int64) { // want "semantic-looking parameter maxBytes uses int64 in internal function SaveAssetForPage; introduce a typed value after parsing"
}

func forbiddenUncheckedMarkdownPathConstructor(raw string) policytree.MarkdownPath {
	return policytree.NewMarkdownPathUnchecked(raw) // want "unchecked constructor NewMarkdownPathUnchecked creates MarkdownPath from primitive in internal code; use a parser or narrow derived-value helper"
}

func allowedFixtureUncheckedConstructor(raw string) policytree.MarkdownPath {
	return NewFixtureMarkdownPath(raw)
}

func NewFixtureMarkdownPath(raw string) policytree.MarkdownPath {
	return policytree.NewMarkdownPathUnchecked(raw)
}

type validationIssuePolicyGap struct {
	Code     IssueCode
	Path     RoutePath
	Message  string // want "message-bearing struct validationIssuePolicyGap exposes Message string without MessageID; add a catalog-backed MessageID"
	Severity IssueSeverity
}

type validationIssueWithMessageID struct {
	Code      IssueCode
	MessageID MessageID
	Message   string
}

type refactorWarningPolicyGap struct {
	Message string // want "message-bearing struct refactorWarningPolicyGap exposes Message string without MessageID; add a catalog-backed MessageID"
}

type refactorPreviewPolicyGap struct {
	Warnings []string // want "message-bearing struct refactorPreviewPolicyGap exposes Warnings strings without MessageID; use catalog-backed warning IDs"
}

func forbiddenWarningCompositeLiteral() refactorPreviewPolicyGap {
	return refactorPreviewPolicyGap{
		Warnings: []string{"Skipped unsupported link syntax"}, // want "warning field Warnings is populated without a MessageID in refactorPreviewPolicyGap; render through catalog-backed warning IDs" "raw localized prose \"Skipped unsupported link syntax\" used in Go contract code; use a catalog-backed message ID or definition"
	}
}

func forbiddenMessageWithoutIDComposite() validationIssueWithMessageID {
	return validationIssueWithMessageID{
		Code:    fixtureIssueCodePolicyGap,
		Message: "Page not found", // want "message field Message is populated without a MessageID in validationIssueWithMessageID; render through a catalog-backed message" "raw localized prose \"Page not found\" used in Go contract code; use a catalog-backed message ID or definition"
	}
}

const fixtureIssueCodePolicyGap IssueCode = "broken_link"

func forbiddenValidationIssueLiteral(destination string) validationIssuePolicyGap {
	return validationIssuePolicyGap{
		Code:     fixtureIssueCodePolicyGap,
		Path:     "/missing",
		Message:  fmt.Sprintf("Destination %s is invalid", destination), // want "message field Message is populated without a MessageID in validationIssuePolicyGap; render through a catalog-backed message" "raw localized prose \"Destination %s is invalid\" used in Go contract code; use a catalog-backed message ID or definition"
		Severity: "error",
	}
}

func forbiddenPrivateWorkspaceWriterLiteral() {
	writePrivateWorkspaceError(403, fixtureErrorCodePageNotFound, "workspace access denied") // want "raw localized prose \"workspace access denied\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenControlWriterLiteral() {
	writeControlError(401, fixtureErrorCodePageNotFound, "invalid api key") // want "raw localized prose \"invalid api key\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenControlWriterSingleWordLiteral() {
	writeControlError(401, fixtureErrorCodePageNotFound, "unauthorized") // want "raw localized prose \"unauthorized\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenFrontdWriterLiteral() {
	writeFrontdError(503, fixtureErrorCodePageNotFound, "workspace unavailable") // want "raw localized prose \"workspace unavailable\" used in Go contract code; use a catalog-backed message ID or definition"
}

func writePrivateWorkspaceError(status int, code ErrorCode, message string) { // want "localized prose sink writePrivateWorkspaceError accepts free-form message string; accept MessageID/catalog args instead"
}

func writeControlError(status int, code ErrorCode, message string) { // want "localized prose sink writeControlError accepts free-form message string; accept MessageID/catalog args instead"
}

func writeFrontdError(status int, code ErrorCode, message string) { // want "localized prose sink writeFrontdError accepts free-form message string; accept MessageID/catalog args instead"
}

func forbiddenValidationIssueErrorString(err error) validationIssuePolicyGap {
	message := err.Error()
	return validationIssuePolicyGap{
		Code:     fixtureIssueCodePolicyGap,
		Path:     "/missing",
		Message:  message, // want "message field Message is populated without a MessageID in validationIssuePolicyGap; render through a catalog-backed message"
		Severity: "error",
	}
}

type workspaceSyncStatusPolicyGap struct {
	LastError        string
	ValidationErrors []validationIssueWithMessageID
}

func forbiddenWorkspaceSyncStatusResponse(status workspaceSyncStatusPolicyGap) H {
	return H{
		"lastError":        status.LastError,        // want "HTTP/private response field lastError forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
		"validationErrors": status.ValidationErrors, // want "HTTP/private response field validationErrors forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
	}
}

func forbiddenPlainMapWorkspaceSyncStatusResponse(status workspaceSyncStatusPolicyGap) map[string]any {
	return map[string]any{
		"lastError":        status.LastError,        // want "HTTP/private response field lastError forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
		"validationErrors": status.ValidationErrors, // want "HTTP/private response field validationErrors forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
	}
}

func forbiddenWrappedPlainMapWorkspaceSyncStatusResponse(status workspaceSyncStatusPolicyGap) map[string]any {
	return map[string]any{
		"lastError":        redactWorkspacePaths(status.LastError),            // want "HTTP/private response field lastError forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
		"validationErrors": redactedValidationErrors(status.ValidationErrors), // want "HTTP/private response field validationErrors forwards message-bearing status text without stable message metadata; render through catalog-backed status details"
	}
}

type privateWorkspaceErrorResponsePolicyGap struct {
	Code      ErrorCode
	MessageID MessageID
	Message   string
}

func forbiddenPrivateWorkspaceErrorSchema(message string) privateWorkspaceErrorResponsePolicyGap {
	return privateWorkspaceErrorResponsePolicyGap{
		Code:      fixtureErrorCodePageNotFound,
		MessageID: MessageIDForCode(fixtureErrorCodePageNotFound),
		Message:   message, // want "message field Message in privateWorkspaceErrorResponsePolicyGap passes through free-form text despite MessageID; render through the catalog instead"
	}
}

func forbiddenLocalizedErrorDetailPassthrough(message string) {
	NewLocalizedErrorDetail(fixtureErrorCodePageNotFound, message, message) // want "localized error constructor NewLocalizedErrorDetail receives free-form message fallback; render through the catalog instead"
}

func forbiddenCLIUnknownCommand(args []string) {
	fmt.Printf("Unknown command: %s\n\n", args[0]) // want "raw localized prose \"Unknown command: %s\\\\n\\\\n\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenCLIResetSuccess() {
	fmt.Println("Admin password reset successfully.") // want "raw localized prose \"Admin password reset successfully.\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenCLINewPassword(username string, password string) {
	fmt.Printf("New password for user %s: %s\n", username, password) // want "raw localized prose \"New password for user %s: %s\\\\n\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenCLIStderrDescriptorRemoval(path string, err error) {
	fmt.Fprintf(os.Stderr, "leafwiki: remove workspace descriptor %s: %v\n", path, err) // want "raw localized prose \"leafwiki: remove workspace descriptor %s: %v\\\\n\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenMCPTransportValidationStdoutReserved() error {
	return fmt.Errorf("stdout is reserved for MCP STDIO") // want "raw localized prose \"stdout is reserved for MCP STDIO\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenMCPTransportValidationAuthIdentity() error {
	return fmt.Errorf("disabled auth and API-key STDIO identity cannot be combined") // want "raw localized prose \"disabled auth and API-key STDIO identity cannot be combined\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenMCPTransportValidationNativeStdio() error {
	return fmt.Errorf("native STDIO requires either disabled auth or an API key") // want "raw localized prose \"native STDIO requires either disabled auth or an API key\" used in Go contract code; use a catalog-backed message ID or definition"
}

func allowedInternalCLIErrorf(err error) error {
	return fmt.Errorf("read hook payload: %w", err)
}

func forbiddenPrivateHTTPError(w http.ResponseWriter) {
	http.Error(w, "workspace access denied", http.StatusForbidden) // want "raw localized prose \"workspace access denied\" used in Go contract code; use a catalog-backed message ID or definition"
}

func forbiddenPrivateHTTPErrorSingleWord(w http.ResponseWriter) {
	http.Error(w, "unauthorized", http.StatusUnauthorized) // want "raw localized prose \"unauthorized\" used in Go contract code; use a catalog-backed message ID or definition"
}

func redactWorkspacePaths(message string) string {
	return message
}

func redactedValidationErrors(errors []validationIssueWithMessageID) []validationIssueWithMessageID {
	return errors
}
