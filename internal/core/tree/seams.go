package tree

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/treemigration"
)

var (
	treeOSStat      = os.Stat
	treeOSReadDir   = os.ReadDir
	treeOSMkdirAll  = os.MkdirAll
	treeOSRename    = os.Rename
	treeOSRemove    = os.Remove
	treeOSRemoveAll = os.RemoveAll
	treeOSReadFile  = os.ReadFile
	treeOSWriteFile = os.WriteFile
	treeOSChtimes   = os.Chtimes

	treeFilepathWalkDir      = filepath.WalkDir
	treeFilepathEvalSymlinks = filepath.EvalSymlinks
	treeFilepathRel          = filepath.Rel
	treeFilepathAbs          = filepath.Abs

	treeJSONMarshal   = json.Marshal
	treeJSONUnmarshal = json.Unmarshal

	treeGenerateUniqueID       = shared.GenerateUniqueID
	treeWriteFileAtomic        = shared.WriteFileAtomic
	treeLoadMarkdownFile       = markdown.LoadMarkdownFile
	treeNewMarkdownFileFromRaw = markdown.NewMarkdownFileFromRaw
	treeMarkdownWriteToFile    = (*markdown.MarkdownFile).WriteToFile

	treeLoadSchema             = loadSchema
	treeSaveSchema             = saveSchema
	treeLoadLegacyTreeSnapshot = loadLegacyTreeSnapshot
	treeRunMigration           = treemigration.Run

	treeStoreReconstructTreeFromFS              = (*NodeStore).ReconstructTreeFromFS
	treeStoreCreatePage                         = (*NodeStore).CreatePage
	treeStoreCreateSection                      = (*NodeStore).CreateSection
	treeStoreUpsertContent                      = (*NodeStore).UpsertContent
	treeStoreUpsertContentPreservingFrontmatter = (*NodeStore).UpsertContentPreservingFrontmatter
	treeStoreUpsertContentReplacingMetadata     = (*NodeStore).UpsertContentReplacingMetadata
	treeStoreMoveNode                           = (*NodeStore).MoveNode
	treeStoreDeletePage                         = (*NodeStore).DeletePage
	treeStoreDeleteSection                      = (*NodeStore).DeleteSection
	treeStoreRenameNode                         = (*NodeStore).RenameNode
	treeStoreReadPageAndRaw                     = (*NodeStore).ReadPageAndRaw
	treeStoreReadPageRaw                        = (*NodeStore).ReadPageRaw
	treeStoreReadPageContent                    = (*NodeStore).ReadPageContent
	treeStoreSyncMetadataIfExists               = (*NodeStore).SyncMetadataIfExists
	treeStoreSaveChildOrder                     = (*NodeStore).SaveChildOrder
	treeStoreConvertNode                        = (*NodeStore).ConvertNode
	treeMapWorkspaceMarkdownRoute               = MapWorkspaceMarkdownRoute
)
