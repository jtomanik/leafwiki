package revision

import (
	"encoding/json"
	"io"
	"os"
	"runtime"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
)

var (
	revisionMkdirAll          = os.MkdirAll
	revisionOpen              = os.Open
	revisionCreateTemp        = os.CreateTemp
	revisionRemove            = os.Remove
	revisionRemoveAll         = os.RemoveAll
	revisionRename            = os.Rename
	revisionReadDir           = os.ReadDir
	revisionReadFile          = os.ReadFile
	revisionStat              = os.Stat
	revisionFileChmod         = (*os.File).Chmod
	revisionFileClose         = (*os.File).Close
	revisionCopy              = io.Copy
	revisionJSONMarshal       = json.Marshal
	revisionJSONMarshalIndent = json.MarshalIndent
	revisionJSONUnmarshal     = json.Unmarshal
	revisionWriteFileAtomic   = shared.WriteFileAtomic
	revisionGenerateUniqueID  = shared.GenerateUniqueID
	revisionGOMAXPROCS        = runtime.GOMAXPROCS

	revisionStoreGetLatestRevision     = (*FSStore).GetLatestRevision
	revisionStoreSaveContentBlob       = (*FSStore).SaveContentBlob
	revisionStoreSaveAssetBlobFromPath = (*FSStore).SaveAssetBlobFromPath
	revisionStoreSaveAssetManifest     = (*FSStore).SaveAssetManifest
	revisionStoreSaveRevision          = (*FSStore).SaveRevision
	revisionStoreListRevisions         = (*FSStore).ListRevisions
	revisionStoreGetRevision           = (*FSStore).GetRevision
	revisionStoreReadContentBlob       = (*FSStore).ReadContentBlob
	revisionStoreLoadAssetManifest     = (*FSStore).LoadAssetManifest
	revisionStoreOpenContentBlob       = (*FSStore).OpenContentBlob
	revisionStoreOpenAssetBlob         = (*FSStore).OpenAssetBlob
	revisionStoreCopyAssetBlobToPath   = (*FSStore).CopyAssetBlobToPath
	revisionStoreDeletePageRevisions   = (*FSStore).DeletePageRevisions
	revisionStoreAssetManifestExists   = (*FSStore).AssetManifestExists

	revisionBuildRestoredRawContent   = buildRestoredRawContent
	revisionUpdateRestoredContent     = (*Service).updateRestoredContent
	revisionRestoreAssets             = (*Service).restoreAssets
	revisionRecordRestoreRevision     = (*Service).recordRestoreRevision
	revisionParsePageDocument         = markdown.ParsePageDocument
	revisionRenderPageDocument        = markdown.RenderPageDocument
	revisionBuildMarkdownWithMetadata = markdown.BuildMarkdownWithMetadata
)
