package wikid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/workspaceid"
)

const HomeWorkspaceID workspaceid.WorkspaceID = "home"

type RegistryService struct {
	store  *RegistryStore
	layout Layout
	now    func() time.Time
}

type RegisterWorkspaceRequest struct {
	DisplayName            string
	DataDir                string
	RootDir                string
	MarkdownLinkRootPrefix string
}

type RegisterWorkspaceResult struct {
	Workspace WorkspaceRecord
	Created   bool
}

func NewRegistryService(store *RegistryStore, layout Layout) *RegistryService {
	return &RegistryService{
		store:  store,
		layout: layout,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (s *RegistryService) BootstrapHome() (WorkspaceRecord, error) {
	return s.BootstrapHomeWorkspace(s.layout.HomeDir, s.layout.HomeRootDir)
}

func (s *RegistryService) BootstrapHomeWorkspace(dataDir string, rootDir string) (WorkspaceRecord, error) {
	var home WorkspaceRecord
	_, err := s.store.Update(func(doc RegistryDocument) (RegistryDocument, error) {
		if existing, ok := doc.Workspace(HomeWorkspaceID); ok {
			existing.DataDir = dataDir
			existing.RootDir = rootDir
			existing.DisplayName = "Home"
			existing.UpdatedAt = s.now()
			doc = replaceWorkspaceRecord(doc, existing)
			home = existing
			return doc, nil
		}
		now := s.now()
		home = normalizeWorkspaceRecord(WorkspaceRecord{
			ID:          HomeWorkspaceID,
			DisplayName: "Home",
			DataDir:     dataDir,
			RootDir:     rootDir,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		doc.Workspaces = append(doc.Workspaces, home)
		return doc, nil
	})
	if err != nil {
		return WorkspaceRecord{}, err
	}
	return home, nil
}

func replaceWorkspaceRecord(doc RegistryDocument, workspace WorkspaceRecord) RegistryDocument {
	for i, existing := range doc.Workspaces {
		if existing.ID == workspace.ID {
			doc.Workspaces[i] = normalizeWorkspaceRecord(workspace)
			return doc
		}
	}
	doc.Workspaces = append(doc.Workspaces, normalizeWorkspaceRecord(workspace))
	return doc
}

func (s *RegistryService) RegisterWorkspace(req RegisterWorkspaceRequest) (WorkspaceRecord, error) {
	result, err := s.RegisterWorkspaceWithResult(req)
	if err != nil {
		return WorkspaceRecord{}, err
	}
	return result.Workspace, nil
}

func (s *RegistryService) RegisterWorkspaceWithResult(req RegisterWorkspaceRequest) (RegisterWorkspaceResult, error) {
	return s.RegisterWorkspaceWithResultAndGrants(req, nil)
}

func (s *RegistryService) RegisterWorkspaceWithResultAndGrants(req RegisterWorkspaceRequest, grants func(RegisterWorkspaceResult) ([]Grant, error)) (RegisterWorkspaceResult, error) {
	workspace, err := s.workspaceRecordForRequest(req)
	if err != nil {
		return RegisterWorkspaceResult{}, err
	}
	return s.store.RegisterWorkspaceWithResultAndGrants(workspace, s.now, grants)
}

func (s *RegistryService) ListWorkspaces() ([]WorkspaceRecord, error) {
	doc, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	workspaces := append([]WorkspaceRecord(nil), doc.Workspaces...)
	sort.SliceStable(workspaces, func(i, j int) bool {
		if workspaces[i].ID == HomeWorkspaceID {
			return true
		}
		if workspaces[j].ID == HomeWorkspaceID {
			return false
		}
		left := strings.ToLower(workspaces[i].DisplayName)
		right := strings.ToLower(workspaces[j].DisplayName)
		if left == right {
			return workspaces[i].ID < workspaces[j].ID
		}
		return left < right
	})
	return workspaces, nil
}

func (s *RegistryService) Workspace(id workspaceid.WorkspaceID) (WorkspaceRecord, bool, error) {
	doc, err := s.store.Load()
	if err != nil {
		return WorkspaceRecord{}, false, err
	}
	workspace, ok := doc.Workspace(id)
	return workspace, ok, nil
}

func (s *RegistryService) workspaceRecordForRequest(req RegisterWorkspaceRequest) (WorkspaceRecord, error) {
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = "Workspace"
	}
	dataDir, err := canonicalRegistryPath(req.DataDir)
	if err != nil {
		return WorkspaceRecord{}, fmt.Errorf("resolve data dir: %w", err)
	}
	rootDir, err := canonicalRegistryPath(req.RootDir)
	if err != nil {
		return WorkspaceRecord{}, fmt.Errorf("resolve root dir: %w", err)
	}
	markdownLinkRootPrefix, err := markdownlinks.NormalizeMarkdownLinkRootPrefix(req.MarkdownLinkRootPrefix)
	if err != nil {
		return WorkspaceRecord{}, fmt.Errorf("normalize markdown link root prefix: %w", err)
	}
	workspaceID, err := workspaceIDFor(displayName, dataDir, rootDir)
	if err != nil {
		return WorkspaceRecord{}, fmt.Errorf("derive workspace ID: %w", err)
	}
	now := s.now()
	return WorkspaceRecord{
		ID:                     workspaceID,
		DisplayName:            displayName,
		DataDir:                dataDir,
		RootDir:                rootDir,
		MarkdownLinkRootPrefix: markdownLinkRootPrefix,
		CreatedAt:              now,
		UpdatedAt:              now,
	}, nil
}

func sameWorkspaceLocation(a WorkspaceRecord, b WorkspaceRecord) bool {
	return filepath.Clean(a.DataDir) == filepath.Clean(b.DataDir) &&
		filepath.Clean(a.RootDir) == filepath.Clean(b.RootDir)
}

func workspaceIDFor(displayName string, dataDir string, rootDir string) (workspaceid.WorkspaceID, error) {
	slug := workspaceSlug(displayName)
	sum := sha256.Sum256([]byte(filepath.Clean(dataDir) + "\x00" + filepath.Clean(rootDir)))
	return workspaceid.ParseWorkspaceID(slug + "-" + hex.EncodeToString(sum[:])[:10])
}

var nonWorkspaceSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func workspaceSlug(displayName string) string {
	slug := strings.ToLower(strings.TrimSpace(displayName))
	slug = nonWorkspaceSlugChars.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "workspace"
	}
	return slug
}
