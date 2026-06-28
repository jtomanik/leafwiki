package gitrevisions

import (
	"crypto/rand"
	"io"
	"os"
	"path/filepath"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/format/index"
	"github.com/go-git/go-git/v6/plumbing/object"
)

var (
	gitRevisionRandRead  = rand.Read
	gitRevisionMkdirAll  = os.MkdirAll
	gitRevisionLstat     = os.Lstat
	gitRevisionReadFile  = os.ReadFile
	gitRevisionRemove    = os.Remove
	gitRevisionWriteFile = os.WriteFile
	gitRevisionWalkDir   = filepath.WalkDir
	gitRevisionRel       = filepath.Rel
	gitRevisionAbs       = filepath.Abs
	gitRevisionReadAll   = io.ReadAll

	gitRevisionGitOpen          = git.Open
	gitRevisionGitInit          = git.Init
	gitRevisionRepoWorktree     = (*git.Repository).Worktree
	gitRevisionRepoHead         = (*git.Repository).Head
	gitRevisionRepoCommitObject = (*git.Repository).CommitObject
	gitRevisionRepoLog          = (*git.Repository).Log
	gitRevisionWorktreeAdd      = (*git.Worktree).Add
	gitRevisionWorktreeRemove   = (*git.Worktree).Remove
	gitRevisionWorktreeCommit   = (*git.Worktree).Commit
	gitRevisionCommitParents    = (*object.Commit).Parents
	gitRevisionCommitTree       = (*object.Commit).Tree
	gitRevisionTreeFiles        = (*object.Tree).Files
	gitRevisionTreeFile         = (*object.Tree).File
	gitRevisionTreeDiffContext  = (*object.Tree).DiffContext
	gitRevisionChangeFiles      = (*object.Change).Files
	gitRevisionFileReader       = (*object.File).Reader
	gitRevisionFileContents     = (*object.File).Contents
	gitRevisionFileIterForEach  = (*object.FileIter).ForEach
	gitRevisionIndexRemove      = (*index.Index).Remove
	gitRevisionRepositoryIndex  = func(repo *git.Repository) (*index.Index, error) {
		return repo.Storer.Index()
	}
	gitRevisionRepositorySetIndex = func(repo *git.Repository, idx *index.Index) error {
		return repo.Storer.SetIndex(idx)
	}
	gitRevisionCommitIterForEach = func(iter object.CommitIter, visit func(*object.Commit) error) error {
		return iter.ForEach(visit)
	}
	gitRevisionCommitIterNext = func(iter object.CommitIter) (*object.Commit, error) {
		return iter.Next()
	}
	gitRevisionStoreStageMarkdownChanges = (*Store).stageMarkdownChanges
	gitRevisionStoreTrackedMarkdownFiles = (*Store).trackedMarkdownFiles
	gitRevisionStoreRemoveFromIndexOnly  = (*Store).removeFromIndexOnly
	gitRevisionStoreFilesAt              = (*Store).FilesAt
	gitRevisionStoreFileContentAt        = (*Store).fileContentAt
	gitRevisionStoreCapture              = (*Store).Capture
	gitRevisionCollectMarkdownPaths      = collectMarkdownPaths
	gitRevisionRemoveInternalRootGitFile = removeInternalRootGitFile
)
