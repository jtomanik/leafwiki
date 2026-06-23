package mcp

import (
	"bytes"
	"encoding/json"

	coreauth "github.com/perber/wiki/internal/core/auth"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	wikipresence "github.com/perber/wiki/internal/wiki/presence"
)

type memoryMultipartFile struct {
	*bytes.Reader
}

func (f *memoryMultipartFile) Close() error {
	return nil
}

type emptyInput struct{}

type configOutput struct {
	PublicAccess            bool   `json:"publicAccess"`
	HideLinkMetadataSection bool   `json:"hideLinkMetadataSection"`
	AuthDisabled            bool   `json:"authDisabled"`
	BasePath                string `json:"basePath"`
	MarkdownLinkRootPrefix  string `json:"markdownLinkRootPrefix"`
	MaxAssetUploadSizeBytes int64  `json:"maxAssetUploadSizeBytes"`
	EnableWorkspaceSync     bool   `json:"enableWorkspaceSync"`
	EnableLinkRefactor      bool   `json:"enableLinkRefactor"`
	HTTPRemoteUserEnabled   bool   `json:"httpRemoteUserEnabled"`
	HTTPRemoteUserLogoutURL string `json:"httpRemoteUserLogoutUrl"`
}

type currentUserOutput struct {
	User *coreauth.PublicUser `json:"user"`
}

type getContextInput struct {
	SinceToken         string `json:"sinceToken,omitempty"`
	SyncMode           string `json:"syncMode,omitempty"`
	TreeDepth          *int   `json:"treeDepth,omitempty"`
	RecentChangesLimit *int   `json:"recentChangesLimit,omitempty"`
}

type contextCheckpointOutput struct {
	Token     string `json:"token"`
	CreatedAt string `json:"createdAt"`
}

type validationSummaryOutput struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

type validationIssueOutput struct {
	Severity wikivalidation.IssueSeverity `json:"severity"`
	Code     wikivalidation.IssueCode     `json:"code"`
	Path     string                       `json:"path,omitempty"`
	PageID   tree.PageID                  `json:"pageId,omitempty"`
	Message  string                       `json:"message"`
}

type validationOutput struct {
	OK      bool                    `json:"ok"`
	Summary validationSummaryOutput `json:"summary"`
	Issues  []validationIssueOutput `json:"issues"`
}

type recentChangeOutput struct {
	CommitID     string        `json:"commitId,omitempty"`
	Timestamp    string        `json:"timestamp,omitempty"`
	Actor        string        `json:"actor,omitempty"`
	Source       string        `json:"source,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	ChangedCount int           `json:"changedCount"`
	ChangedPaths []string      `json:"changedPaths"`
	PageIDs      []tree.PageID `json:"pageIds,omitempty"`
}

type presenceStatusOutput struct {
	Web        string `json:"web"`
	AgentHooks string `json:"agentHooks"`
}

type contextOutput struct {
	ContextToken                string                    `json:"contextToken"`
	PreviousContextToken        string                    `json:"previousContextToken"`
	ChangesSincePreviousContext []recentChangeOutput      `json:"changesSincePreviousContext"`
	ContextHistory              []contextCheckpointOutput `json:"contextHistory"`
	User                        *coreauth.PublicUser      `json:"user"`
	Config                      configOutput              `json:"config"`
	Server                      map[string]any            `json:"server"`
	SyncStatus                  any                       `json:"syncStatus"`
	Validation                  validationOutput          `json:"validation"`
	RecentChanges               []recentChangeOutput      `json:"recentChanges"`
	ActiveSessions              []wikipresence.Session    `json:"activeSessions"`
	PresenceStatus              presenceStatusOutput      `json:"presenceStatus"`
	Tree                        *dto.Node                 `json:"tree"`
	RecommendedTools            []ToolID                  `json:"recommendedTools"`
	CanonicalLinkExamples       []string                  `json:"canonicalLinkExamples"`
	Warnings                    []string                  `json:"warnings,omitempty"`
}

type refreshInput struct {
	Validate *bool  `json:"validate,omitempty"`
	Source   string `json:"source,omitempty"`
}

type refreshOutput struct {
	SyncStatus         any               `json:"syncStatus"`
	RecentChangedPaths []string          `json:"recentChangedPaths"`
	Validation         *validationOutput `json:"validation,omitempty"`
	LastCommitHash     string            `json:"lastCommitHash"`
}

type getSubtreeInput struct {
	PageID                string `json:"pageId,omitempty"`
	Path                  string `json:"path,omitempty"`
	Depth                 *int   `json:"depth,omitempty"`
	IncludeMetadata       *bool  `json:"includeMetadata,omitempty"`
	IncludeLinkCounts     bool   `json:"includeLinkCounts,omitempty"`
	IncludeContentPreview bool   `json:"includeContentPreview,omitempty"`
}

type subtreeOutput struct {
	Root        *subtreeNode   `json:"root"`
	Breadcrumbs []*subtreeNode `json:"breadcrumbs"`
	Depth       int            `json:"depth"`
	Truncated   bool           `json:"truncated"`
}

type subtreeNode struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Slug           string            `json:"slug"`
	Path           string            `json:"path"`
	Version        string            `json:"version"`
	Position       int               `json:"position"`
	Kind           tree.NodeKind     `json:"kind"`
	Children       []*subtreeNode    `json:"children"`
	Metadata       *dto.NodeMetadata `json:"metadata,omitempty"`
	LinkCounts     any               `json:"linkCounts,omitempty"`
	ContentPreview string            `json:"contentPreview,omitempty"`
}

type validatePageInput struct {
	PageID string `json:"pageId,omitempty"`
	Path   string `json:"path,omitempty"`
	Kind   string `json:"kind,omitempty"`
}

type validateContentInput struct {
	Path           string `json:"path"`
	Content        string `json:"content"`
	ExistingPageID string `json:"existingPageId,omitempty"`
	Kind           string `json:"kind,omitempty"`
}

type validateWikiInput struct {
	IncludeWarnings *bool `json:"includeWarnings,omitempty"`
}

type updatePageMetadataInput struct {
	PageID            string            `json:"pageId,omitempty"`
	Path              string            `json:"path,omitempty"`
	Version           string            `json:"version"`
	SetTags           []string          `json:"setTags,omitempty"`
	AddTags           []string          `json:"addTags,omitempty"`
	RemoveTags        []string          `json:"removeTags,omitempty"`
	SetProperties     map[string]string `json:"setProperties,omitempty"`
	RemoveProperties  []string          `json:"removeProperties,omitempty"`
	IncludePage       bool              `json:"includePage,omitempty"`
	IncludeValidation *bool             `json:"includeValidation,omitempty"`
	IncludeLinkStatus bool              `json:"includeLinkStatus,omitempty"`
}

type replacePageSectionInput struct {
	PageID            string   `json:"pageId,omitempty"`
	Path              string   `json:"path,omitempty"`
	Version           string   `json:"version"`
	HeadingPath       []string `json:"headingPath"`
	Occurrence        int      `json:"occurrence,omitempty"`
	Content           string   `json:"content"`
	IncludePage       bool     `json:"includePage,omitempty"`
	IncludeValidation *bool    `json:"includeValidation,omitempty"`
	IncludeLinkStatus bool     `json:"includeLinkStatus,omitempty"`
}

type partialEditOutput struct {
	PageID     string            `json:"pageId"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Version    string            `json:"version"`
	Validation *validationOutput `json:"validation,omitempty"`
	Page       *dto.Page         `json:"page,omitempty"`
	LinkStatus any               `json:"linkStatus,omitempty"`
}

type getTreeInput struct {
	Depth *int `json:"depth,omitempty"`
}

type treeOutput struct {
	Tree *dto.Node `json:"tree"`
}

type pageIDInput struct {
	ID     string `json:"id,omitempty"`
	PageID string `json:"pageId,omitempty"`
}

type pathInput struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

type pagePathInput struct {
	Path string `json:"path"`
	Kind string `json:"kind,omitempty"`
}

type lookupPathOutput struct {
	Lookup *tree.PathLookup `json:"lookup"`
}

type resolvePermalinkOutput struct {
	Target *tree.PermalinkTarget `json:"target"`
}

type suggestSlugInput struct {
	ParentID  string `json:"parentId,omitempty"`
	CurrentID string `json:"currentId,omitempty"`
	Title     string `json:"title"`
}

type suggestSlugOutput struct {
	Slug string `json:"slug"`
}

type createPageInput struct {
	ParentID *string `json:"parentId,omitempty"`
	Title    string  `json:"title"`
	Slug     string  `json:"slug"`
	Kind     *string `json:"kind,omitempty"`
}

type updatePageInput struct {
	ID                string            `json:"id"`
	Version           string            `json:"version"`
	Title             string            `json:"title"`
	Slug              string            `json:"slug"`
	Content           *string           `json:"content,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Properties        map[string]string `json:"properties,omitempty"`
	TagsPresent       bool              `json:"-"`
	PropertiesPresent bool              `json:"-"`
}

func (in *updatePageInput) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID         string           `json:"id"`
		Version    string           `json:"version"`
		Title      string           `json:"title"`
		Slug       string           `json:"slug"`
		Content    *string          `json:"content,omitempty"`
		Tags       *json.RawMessage `json:"tags,omitempty"`
		Properties *json.RawMessage `json:"properties,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	in.ID = raw.ID
	in.Version = raw.Version
	in.Title = raw.Title
	in.Slug = raw.Slug
	in.Content = raw.Content
	in.Tags = nil
	in.Properties = nil
	in.TagsPresent = raw.Tags != nil
	in.PropertiesPresent = raw.Properties != nil

	if raw.Tags != nil && string(*raw.Tags) != "null" {
		if err := json.Unmarshal(*raw.Tags, &in.Tags); err != nil {
			return err
		}
	}
	if raw.Properties != nil && string(*raw.Properties) != "null" {
		if err := json.Unmarshal(*raw.Properties, &in.Properties); err != nil {
			return err
		}
	}
	return nil
}

type pageOutput struct {
	Page       *dto.Page `json:"page"`
	LinkStatus any       `json:"linkStatus,omitempty"`
}

type deletePageInput struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Recursive bool   `json:"recursive,omitempty"`
}

type movePageInput struct {
	ID       string  `json:"id"`
	Version  string  `json:"version"`
	ParentID *string `json:"parentId,omitempty"`
}

type sortPagesInput struct {
	ParentID   string   `json:"parentId"`
	OrderedIDs []string `json:"orderedIds"`
}

type ensurePageInput struct {
	Path  string  `json:"path"`
	Title string  `json:"title"`
	Kind  *string `json:"kind,omitempty"`
}

type convertPageInput struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	TargetKind string `json:"targetKind"`
}

type copyPageInput struct {
	ID             string  `json:"id"`
	TargetParentID *string `json:"targetParentId,omitempty"`
	Title          string  `json:"title"`
	Slug           string  `json:"slug"`
}

type messageOutput struct {
	MessageID ToolMessageID `json:"messageId"`
	Message   string        `json:"message"`
}

type ToolMessageID string

func (id ToolMessageID) String() string {
	return string(id)
}

const (
	ToolMessageDeletePageSuccess  ToolMessageID = "mcp.tools.wiki_delete_page.success"
	ToolMessageMovePageSuccess    ToolMessageID = "mcp.tools.wiki_move_page.success"
	ToolMessageSortPagesSuccess   ToolMessageID = "mcp.tools.wiki_sort_pages.success"
	ToolMessageConvertPageSuccess ToolMessageID = "mcp.tools.wiki_convert_page.success"
	ToolMessageDeleteAssetSuccess ToolMessageID = "mcp.tools.wiki_delete_asset.success"
)

func newMessageOutput(messageID ToolMessageID, message string) messageOutput {
	return messageOutput{MessageID: messageID, Message: message}
}

type searchPagesInput struct {
	Query  string   `json:"q,omitempty"`
	Tags   []string `json:"tags,omitempty"`
	Offset int      `json:"offset,omitempty"`
	Limit  int      `json:"limit,omitempty"`
}

type searchPagesOutput struct {
	Count     int  `json:"count"`
	Items     any  `json:"items"`
	Limit     int  `json:"limit"`
	Offset    int  `json:"offset"`
	TagFacets any  `json:"tagFacets"`
	HasMore   bool `json:"hasMore"`
}

type searchStatusOutput struct {
	Status any `json:"status"`
}

type listTagsInput struct {
	Query    string   `json:"q,omitempty"`
	Selected []string `json:"selected,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

type listTagsOutput struct {
	Tags any `json:"tags"`
}

type pagesByTagsInput struct {
	Tags []string `json:"tags"`
}

type pagesOutput struct {
	Pages any `json:"pages"`
}

type listPropertyKeysInput struct {
	Query string `json:"q,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type propertyKeysOutput struct {
	Keys any `json:"keys"`
}

type pagesByPropertyInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type linkStatusOutput struct {
	Status any `json:"status"`
}

type uploadAssetInput struct {
	PageID        string `json:"pageId"`
	Filename      string `json:"filename"`
	ContentBase64 string `json:"contentBase64"`
}

type uploadAssetOutput struct {
	File string `json:"file"`
}

type assetInput struct {
	PageID   string `json:"pageId"`
	Filename string `json:"filename"`
}

type assetOutput struct {
	Filename      string `json:"filename"`
	MimeType      string `json:"mimeType"`
	ContentBase64 string `json:"contentBase64"`
}

type listAssetsOutput struct {
	Files []string `json:"files"`
}

type renameAssetInput struct {
	PageID      string `json:"pageId"`
	OldFilename string `json:"oldFilename"`
	NewFilename string `json:"newFilename"`
}

type renameAssetOutput struct {
	URL string `json:"url"`
}

type deleteAssetInput struct {
	PageID   string `json:"pageId"`
	Filename string `json:"filename"`
}

type listRevisionsInput struct {
	ID     string `json:"id,omitempty"`
	PageID string `json:"pageId,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

type listRevisionsOutput struct {
	Revisions  any    `json:"revisions"`
	NextCursor string `json:"nextCursor"`
}

type revisionIDInput struct {
	ID         string `json:"id,omitempty"`
	PageID     string `json:"pageId,omitempty"`
	RevisionID string `json:"revisionId,omitempty"`
}

type revisionOutput struct {
	Revision any `json:"revision"`
}

type compareRevisionsInput struct {
	ID               string `json:"id,omitempty"`
	PageID           string `json:"pageId,omitempty"`
	BaseRevisionID   string `json:"baseRevisionId"`
	TargetRevisionID string `json:"targetRevisionId"`
}

type revisionAssetInput struct {
	ID         string `json:"id,omitempty"`
	PageID     string `json:"pageId,omitempty"`
	RevisionID string `json:"revisionId"`
	AssetName  string `json:"assetName"`
}

type previewRefactorInput struct {
	ID       string  `json:"id,omitempty"`
	PageID   string  `json:"pageId,omitempty"`
	Kind     string  `json:"kind"`
	Title    string  `json:"title,omitempty"`
	Slug     string  `json:"slug,omitempty"`
	Content  *string `json:"content,omitempty"`
	ParentID *string `json:"parentId,omitempty"`
}

type applyRefactorInput struct {
	ID           string  `json:"id,omitempty"`
	PageID       string  `json:"pageId,omitempty"`
	Version      string  `json:"version,omitempty"`
	Kind         string  `json:"kind"`
	Title        string  `json:"title,omitempty"`
	Slug         string  `json:"slug,omitempty"`
	Content      *string `json:"content,omitempty"`
	ParentID     *string `json:"parentId,omitempty"`
	RewriteLinks bool    `json:"rewriteLinks,omitempty"`
}
