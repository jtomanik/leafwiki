package semanticcases

import "net/url"

func allowedPackageURLPathEscape(workspaceID WorkspaceID) string {
	return url.PathEscape(string(workspaceID))
}

func forbiddenLocalPathEscape(workspaceID WorkspaceID) string {
	return PathEscape(string(workspaceID)) // want "semantic value WorkspaceID converted to string before internal call PathEscape; make the callee accept WorkspaceID"
}

func forbiddenPackagePathEscapeMask(service PageService, workspaceID WorkspaceID) {
	service.findByID(url.PathEscape(string(workspaceID))) // want "semantic value WorkspaceID converted to string before internal call findByID; make the callee accept WorkspaceID"
}

func forbiddenPackagePathEscapeLocal(service PageService, workspaceID WorkspaceID) {
	escaped := url.PathEscape(string(workspaceID)) // want "semantic value WorkspaceID converted to string into local escaped; keep WorkspaceID typed until an explicit boundary"
	service.findByID(escaped)
}

func PathEscape(value string) string {
	return value
}
