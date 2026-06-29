package tree

import (
	"errors"
	"fmt"
)

var ErrPageNotFound = errors.New("page not found")
var ErrParentNotFound = errors.New("parent not found")
var ErrTreeNotLoaded = errors.New("tree not loaded")
var ErrTreeReconstructionNil = errors.New("tree reconstruction returned nil")
var ErrPageHasChildren = errors.New("page has children")
var ErrPageAlreadyExists = errors.New("page already exists")
var ErrMovePageCircularReference = errors.New("circular reference detected")
var ErrPageCannotBeMovedToItself = errors.New("page cannot be moved to itself")
var ErrInvalidSortOrder = errors.New("invalid sort order")
var ErrFileNotFound = errors.New("file not found")
var ErrDrift = errors.New("drift detected")
var ErrInvalidOperation = errors.New("invalid operation")
var ErrConvertNotAllowed = errors.New("convert not allowed")
var ErrVersionConflict = errors.New("version conflict")
var ErrVersionRequired = errors.New("version required")
var ErrResolvePath = errors.New("resolve path")
var ErrEnsureFolder = errors.New("could not ensure folder")
var ErrLoadMarkdownFile = errors.New("could not load markdown file")
var ErrReadTreeFile = errors.New("read tree file")
var ErrUnmarshalTreeData = errors.New("unmarshal tree data")
var ErrRootPathNotDirectory = errors.New("is not a directory")
var ErrEmptyLeafwikiID = errors.New("empty leafwiki_id")
var ErrDuplicateLeafwikiID = errors.New("duplicate leafwiki_id")
var ErrMissingRoutePath = errors.New("missing path")
var ErrInvalidRoutePath = errors.New("invalid path")
var ErrSlugEmpty = errors.New("slug must not be empty")
var ErrScanPageID = errors.New("page ID scan source not supported")
var ErrScanRoutePath = errors.New("route path scan source not supported")
var ErrInvalidMigrationNode = errors.New("invalid migration node")
var ErrGetPageContent = errors.New("could not get page content")
var ErrEnsurePagePath = errors.New("could not ensure page path")
var ErrCollectLegacyContentFiles = errors.New("collect legacy content files")
var ErrLegacyContentRemains = errors.New("legacy content remains")
var ErrReadLegacyContentPath = errors.New("read legacy content path")
var ErrReadConfiguredLegacyContentPath = errors.New("read configured legacy content path")
var ErrReadDirectory = errors.New("read directory")
var ErrLegacyUnknownKind = errors.New("unknown kind")
var ErrLegacySnapshotTreeRequired = errors.New("legacy snapshot requires loaded tree")
var ErrMarshalLegacyTreeSnapshot = errors.New("marshal legacy tree snapshot")
var ErrWriteLegacyTreeSnapshot = errors.New("write legacy tree snapshot")
var ErrStatLegacyContentPath = errors.New("stat legacy content path")
var ErrStatConfiguredLegacyContentPath = errors.New("stat configured legacy content path")
var ErrLoadConfiguredLegacyContentPath = errors.New("load configured legacy content path")
var ErrDuplicateReconstructedSlug = errors.New("duplicate reconstructed slug")
var ErrResolveRootDir = errors.New("resolve root dir")
var ErrGenerateUniqueID = errors.New("could not generate unique ID")
var ErrCreatePageEntry = errors.New("could not create page entry")
var ErrCreateSectionEntry = errors.New("could not create section entry")
var ErrPersistChildOrder = errors.New("could not persist child order")
var ErrRollbackCreatedNode = errors.New("rollback created node")
var ErrRestoreContent = errors.New("could not restore content")
var ErrSyncRestoredMetadata = errors.New("could not sync restored metadata")
var ErrParentMustBeSection = errors.New("cannot add child to non-section parent")
var ErrConvertParentNode = errors.New("could not convert parent node")
var ErrGetPageRawContent = errors.New("could not get page raw content")
var ErrLookupPagePath = errors.New("could not lookup page path")
var ErrFindExistingPageByID = errors.New("could not find existing page by ID")
var ErrFindCreatedPageByID = errors.New("could not find created page by ID")
var ErrCreateSegment = errors.New("could not create segment")
var ErrPersistSourceChildOrder = errors.New("could not persist source child order")
var ErrPersistDestinationChildOrder = errors.New("could not persist destination child order")
var ErrSyncMovedNodeMetadata = errors.New("could not sync moved node metadata")
var ErrRollbackMovedNode = errors.New("rollback moved node")
var ErrMoveNodeBackOnDisk = errors.New("move node back on disk")
var ErrResolveConvertedParentDir = errors.New("resolve converted parent dir")
var ErrRemoveChildOrderBeforeParentRollback = errors.New("remove child order before parent rollback")
var ErrConvertDestinationParentBackToPage = errors.New("convert destination parent back to page")
var ErrWriteMarkdownFile = errors.New("could not write markdown file")

// versionUnchecked bypasses optimistic locking for internal system operations
// that have no client-supplied version (e.g. revision restore, copy cleanup).
const versionUnchecked = "\x00"

// DriftError represents a drift error with detailed information.
type DriftError struct {
	NodeID PageID
	Kind   NodeKind
	Path   string
	Reason string
}

func (e *DriftError) Error() string {
	return fmt.Sprintf("drift detected: nodeID=%s, kind=%s, path=%s, reason=%s", e.NodeID, e.Kind, e.Path, e.Reason)
}

func (e *DriftError) Unwrap() error {
	return ErrDrift
}

// InvalidOpError represents an invalid operation error with details.
type InvalidOpError struct {
	Op     string
	Reason string
}

func (e *InvalidOpError) Error() string { return fmt.Sprintf("%s: %s", e.Op, e.Reason) }
func (e *InvalidOpError) Unwrap() error { return ErrInvalidOperation }

// PageAlreadyExistsError: Konflikt bei Create/Move/Rename
type PageAlreadyExistsError struct {
	Path string
}

func (e *PageAlreadyExistsError) Error() string { return fmt.Sprintf("already exists: %s", e.Path) }
func (e *PageAlreadyExistsError) Unwrap() error { return ErrPageAlreadyExists }

// NotFoundError represents a not found error with details.
type NotFoundError struct {
	Resource string
	ID       PageID
	Path     string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Resource, e.ID)
}

func (e *NotFoundError) Unwrap() error {
	return ErrPageNotFound
}

// ConvertNotAllowedError represents a convert not allowed error with details.
type ConvertNotAllowedError struct {
	From   NodeKind
	To     NodeKind
	Reason string
}

func (e *ConvertNotAllowedError) Error() string {
	return fmt.Sprintf("cannot convert from %s to %s: %s", e.From, e.To, e.Reason)
}

func (e *ConvertNotAllowedError) Unwrap() error {
	return ErrConvertNotAllowed
}
