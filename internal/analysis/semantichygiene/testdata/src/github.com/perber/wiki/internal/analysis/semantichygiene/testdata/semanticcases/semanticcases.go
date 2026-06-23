package semanticcases

type PageID string

func (id PageID) String() string {
	return string(id)
}

type UserID string

func (id UserID) String() string {
	return string(id)
}

type PageVersion string

func (version PageVersion) String() string {
	return string(version)
}

type RoutePath string

func (path RoutePath) String() string {
	return string(path)
}

type WorkspaceID string

type RevisionID string

type Slug string

type AssetName string

type WorkspaceSourcePath string

type APIKeyID string

type ErrorCode string

func (code ErrorCode) String() string {
	return string(code)
}

type FieldErrorCode string

type IssueCode string

type MessageID string

func (id MessageID) String() string {
	return string(id)
}

type ToolID string

func (id ToolID) String() string {
	return string(id)
}

type PageService struct{}

func (PageService) findByID(id string) { // want "semantic-looking parameter id uses string in internal function findByID; use PageID or accept a DTO boundary value"
}

func forbiddenStringCall(service PageService, pageID PageID) {
	service.findByID(pageID.String()) // want "semantic value PageID converted to string before internal call findByID; make the callee accept PageID"
}

func forbiddenStringValidateCall(pageID PageID) {
	ValidateText(pageID.String()) // want "semantic value PageID converted to string before internal call ValidateText; make the callee accept PageID"
}

func forbiddenStringTrimCall(pageID PageID) string {
	return TrimSpace(pageID.String()) // want "semantic value PageID converted to string before internal call TrimSpace; make the callee accept PageID"
}

func forbiddenStringWithCall(pageID PageID) {
	With("pageID", pageID.String()) // want "semantic value PageID converted to string before internal call With; make the callee accept PageID"
}

func forbiddenRoutePathParse(routePath RoutePath) {
	ParseRoutePath(routePath.String()) // want "semantic value RoutePath converted to string before internal call ParseRoutePath; make the callee accept RoutePath"
}

func forbiddenRoutePathSprintf(routePath RoutePath) string {
	return Sprintf("%s", routePath.String()) // want "semantic value RoutePath converted to string before internal call Sprintf; make the callee accept RoutePath"
}

func forbiddenStringConcatCall(service PageService, pageID PageID) {
	service.findByID("page:" + pageID.String()) // want "semantic value PageID converted to string before internal call findByID; make the callee accept PageID"
}

func forbiddenStringParen(pageID PageID) string {
	value := (pageID.String()) // want "semantic value PageID converted to string into local value; keep PageID typed until an explicit boundary"
	return value
}

func forbiddenSliceLiteral(pageID PageID) []string {
	return []string{pageID.String()} // want "semantic value PageID converted to string for semantic field composite literal; keep the field typed or convert only at a boundary"
}

func forbiddenMapKey(pageID PageID) map[string]bool {
	return map[string]bool{pageID.String(): true} // want "semantic value PageID converted to string into local map key; keep PageID typed until an explicit boundary"
}

func forbiddenErrorfWrapper(routePath RoutePath) {
	Errorf("%s", routePath.String()) // want "semantic value RoutePath converted to string before internal call Errorf; make the callee accept RoutePath"
}

func Errorf(format string, values ...any) {}

type writerWrapper struct{}

func (writerWrapper) WriteString(value string) {}

func forbiddenWriteStringWrapper(writer writerWrapper, routePath RoutePath) {
	writer.WriteString(routePath.String()) // want "semantic value RoutePath converted to string before internal call WriteString; make the callee accept RoutePath"
}

func ValidateText(value string) {}

func TrimSpace(value string) string {
	return value
}

func With(key string, value string) {}

func ParseRoutePath(value string) (RoutePath, error) {
	return RoutePath(value), nil
}

func Sprintf(format string, values ...any) string {
	return ""
}

func forbiddenStringComparison(left PageID, right string) bool {
	return left.String() == right // want "semantic value PageID converted to string for comparison; compare PageID values directly or parse the primitive first"
}

type domainRecord struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type domainRecord; use PageID or mark the type as a DTO boundary"
}

func forbiddenDomainAssignment(record *domainRecord, pageID PageID) {
	record.PageID = pageID.String() // want "semantic value PageID converted to string for semantic field PageID; keep the field typed or convert only at a boundary"
}

func forbiddenStringTemporary(service PageService, pageID PageID) {
	tmp := pageID.String() // want "semantic value PageID converted to string into local tmp; keep PageID typed until an explicit boundary"
	service.findByID(tmp)
}

func forbiddenStringReturn(pageID PageID) string {
	return pageID.String() // want "semantic value PageID returned as string from internal function forbiddenStringReturn; return PageID or serialize only at a boundary"
}

func forbiddenCompositeAssignment(pageID PageID) domainRecord {
	return domainRecord{PageID: pageID.String()} // want "semantic value PageID converted to string for semantic field PageID; keep the field typed or convert only at a boundary"
}

func forbiddenPositionalComposite(pageID PageID) domainRecord {
	return domainRecord{pageID.String()} // want "semantic value PageID converted to string for semantic field composite literal; keep the field typed or convert only at a boundary"
}

type routeEntry struct {
	Kind        string
	Path        string
	ContentPath string
}

func forbiddenRoutePathStringConversion(routePath RoutePath, relPath string) routeEntry {
	return routeEntry{Kind: "page", Path: string(routePath) + ".md", ContentPath: relPath} // want "semantic value RoutePath converted to string for semantic field Path; keep the field typed or convert only at a boundary"
}

func forbiddenRoutePathStringMethodEntry(routePath RoutePath, relPath string) routeEntry {
	return routeEntry{Kind: "page", Path: routePath.String() + ".md", ContentPath: relPath} // want "semantic value RoutePath converted to string for semantic field Path; keep the field typed or convert only at a boundary"
}

func forbiddenRoutePathStringLocal(routePath RoutePath) string {
	path := string(routePath) // want "semantic value RoutePath converted to string into local path; keep RoutePath typed until an explicit boundary"
	return path
}

type taggedDomainRouteRecord struct {
	RoutePath string `json:"routePath"` // want "semantic-looking field RoutePath uses string in domain/service type taggedDomainRouteRecord; use RoutePath or mark the type as a DTO boundary"
}

func forbiddenTaggedDomainRoute(routePath RoutePath) taggedDomainRouteRecord {
	return taggedDomainRouteRecord{RoutePath: routePath.String()} // want "semantic value RoutePath converted to string for semantic field RoutePath; keep the field typed or convert only at a boundary"
}

type pageResponse struct {
	PageID string `json:"pageId"`
}

type domainResponse struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type domainResponse; use PageID or mark the type as a DTO boundary"
}

func allowedDTOAssignment(pageID PageID) pageResponse {
	return pageResponse{PageID: pageID.String()}
}

func forbiddenDTOWrapper(pageID PageID) pageResponse {
	return pageResponse{PageID: normalizePageID(pageID.String())} // want "semantic value PageID converted to string before internal call normalizePageID; make the callee accept PageID"
}

func normalizePageID(value string) string {
	return value
}

func forbiddenDirectCast(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

const rawPageID = "page-1"

func forbiddenLiteralDirectCast() PageID {
	return PageID("page-1") // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func forbiddenConstDirectCast() PageID {
	return PageID(rawPageID) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func forbiddenRoutePathDirectCast(raw string) RoutePath {
	return RoutePath(raw) // want "direct cast to semantic type RoutePath outside parser or boundary; use a parser or typed input"
}

func ParsePageID(raw string) (PageID, error) {
	return PageID(raw), nil
}

func ParseAnything(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func NewPageIDWithoutValidation(raw string) PageID {
	return PageID(raw) // want "direct cast to semantic type PageID outside parser or boundary; use a parser or typed input"
}

func forbiddenRouteValidator(rawRoute string) (string, error) {
	return rawRoute, nil
}

func ValidateRoutePath(routePath string) (string, error) { // want "validator ValidateRoutePath returns primitive string for semantic routePath; return RoutePath or rename the function if it does not validate a semantic value"
	return routePath, nil
}

func ValidateSlug(raw string) (string, error) { // want "validator ValidateSlug returns primitive string for semantic Slug; return Slug or rename the function if it does not validate a semantic value"
	return raw, nil
}

type serviceInput struct {
	PageID string // want "semantic-looking field PageID uses string in domain/service type serviceInput; use PageID or mark the type as a DTO boundary"
}

type serviceIndex struct {
	PageIDs   []string        // want "semantic-looking field PageIDs uses string in domain/service type serviceIndex; use PageID or mark the type as a DTO boundary"
	PagesByID map[string]bool // want "semantic-looking field PagesByID uses string in domain/service type serviceIndex; use PageID or mark the type as a DTO boundary"
}

type broaderServiceInput struct {
	WorkspaceID string // want "semantic-looking field WorkspaceID uses string in domain/service type broaderServiceInput; use WorkspaceID or mark the type as a DTO boundary"
	RevisionID  string // want "semantic-looking field RevisionID uses string in domain/service type broaderServiceInput; use RevisionID or mark the type as a DTO boundary"
	RoutePath   string // want "semantic-looking field RoutePath uses string in domain/service type broaderServiceInput; use RoutePath or mark the type as a DTO boundary"
	Slug        string // want "semantic-looking field Slug uses string in domain/service type broaderServiceInput; use Slug or mark the type as a DTO boundary"
	AssetName   string // want "semantic-looking field AssetName uses string in domain/service type broaderServiceInput; use AssetName or mark the type as a DTO boundary"
	Filename    string // want "semantic-looking field Filename uses string in domain/service type broaderServiceInput; use AssetName or mark the type as a DTO boundary"
	SourcePath  string // want "semantic-looking field SourcePath uses string in domain/service type broaderServiceInput; use WorkspaceSourcePath or mark the type as a DTO boundary"
	UserID      string // want "semantic-looking field UserID uses string in domain/service type broaderServiceInput; use UserID or mark the type as a DTO boundary"
	APIKeyID    string // want "semantic-looking field APIKeyID uses string in domain/service type broaderServiceInput; use APIKeyID or mark the type as a DTO boundary"
}

type typedServiceIndex struct {
	PageID    PageID
	PageIDs   []PageID
	PagesByID map[PageID]bool
}

type requestDTO struct {
	PageID  string   `json:"pageId"`
	PageIDs []string `json:"pageIds"`
}

func loadPage(pageID string) { // want "semantic-looking parameter pageID uses string in internal function loadPage; use PageID or accept a DTO boundary value"
}

func loadPages(pageIDs []string) { // want "semantic-looking parameter pageIDs uses string in internal function loadPages; use PageID or accept a DTO boundary value"
}

func loadPageVersion(pageVersion string) { // want "semantic-looking parameter pageVersion uses string in internal function loadPageVersion; use PageVersion or accept a DTO boundary value"
}

func loadWorkspace(workspaceID string) { // want "semantic-looking parameter workspaceID uses string in internal function loadWorkspace; use WorkspaceID or accept a DTO boundary value"
}

func loadRevision(revisionID string) { // want "semantic-looking parameter revisionID uses string in internal function loadRevision; use RevisionID or accept a DTO boundary value"
}

func resolveRoute(routePath string) { // want "semantic-looking parameter routePath uses string in internal function resolveRoute; use RoutePath or accept a DTO boundary value"
}

func updateSlug(slug string) { // want "semantic-looking parameter slug uses string in internal function updateSlug; use Slug or accept a DTO boundary value"
}

func loadAsset(assetName string) { // want "semantic-looking parameter assetName uses string in internal function loadAsset; use AssetName or accept a DTO boundary value"
}

func deleteFilename(filename string) { // want "semantic-looking parameter filename uses string in internal function deleteFilename; use AssetName or accept a DTO boundary value"
}

func loadSource(sourcePath string) { // want "semantic-looking parameter sourcePath uses string in internal function loadSource; use WorkspaceSourcePath or accept a DTO boundary value"
}

func createAPIKey(apiKeyID string) { // want "semantic-looking parameter apiKeyID uses string in internal function createAPIKey; use APIKeyID or accept a DTO boundary value"
}

func GetUserByID(id string) { // want "semantic-looking parameter id uses string in internal function GetUserByID; use UserID or accept a DTO boundary value"
}

func loadTypedPage(pageID PageID) {
}

func loadTypedPages(pageIDs []PageID, pagesByID map[PageID]bool) {
}

func allowedWire(pageID string) requestDTO {
	return requestDTO{PageID: pageID, PageIDs: []string{pageID}}
}

func forbiddenErrorCodeLiteral() {
	respondError("page_not_found") // want "raw stable contract literal \"page_not_found\" used in production code; use the typed constant or definition"
}

func respondError(code string) {}

func forbiddenFieldCodeLiteral() {
	respondFieldCode("page_slug_invalid") // want "raw stable contract literal \"page_slug_invalid\" used in production code; use the typed constant or definition"
}

func respondFieldCode(code FieldErrorCode) {}

func forbiddenIssueCodeLiteral() {
	recordIssue("broken_link") // want "raw stable contract literal \"broken_link\" used in production code; use the typed constant or definition"
}

func recordIssue(code IssueCode) {}

func allowedNonContractSnakeLiteral() string {
	return "api_key"
}

const ErrCodePageNotFound ErrorCode = "page_not_found"

func allowedErrorCodeConstant() ErrorCode {
	return ErrCodePageNotFound
}

func forbiddenToolLiteral() {
	registerTool("wiki_get_page") // want "raw stable contract literal \"wiki_get_page\" used in production code; use the typed constant or definition"
}

func registerTool(name string) {}

const ToolGetPage ToolID = "wiki_get_page"

func forbiddenMessageLiteral() {
	respondMessageID("mcp.tools.wiki_get_page.success") // want "raw stable contract literal \"mcp.tools.wiki_get_page.success\" used in production code; use the typed constant or definition"
}

func allowedMessagePrefixFragment() string {
	return "mcp.tools."
}

func respondMessageID(messageID string) {}

func MessageIDForCode(code ErrorCode) MessageID {
	return MessageID("errors." + string(code))
}
