package importer

import "os"

var zipWorkspaceRemoveAll = os.RemoveAll

type ZipWorkspace struct {
	Root string
}

func (ws *ZipWorkspace) Cleanup() error {
	if ws == nil || ws.Root == "" {
		return nil
	}
	return zipWorkspaceRemoveAll(ws.Root)
}
