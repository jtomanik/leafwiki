package http

import "io/fs"

// Re-export unexported helpers for use in external test package (package http_test).

var BuildCustomStylesheetTag = buildCustomStylesheetTag
var FrontendIndexHTML = frontendIndexHTML
var FrontendRequestPath = frontendRequestPath
var InjectIntoHead = injectIntoHead
var IsFrontendSPARoute = isFrontendSPARoute

func SetFrontendSubFSForTest(fn func(fs.FS, string) (fs.FS, error)) func() {
	previous := frontendSubFS
	frontendSubFS = fn
	return func() {
		frontendSubFS = previous
	}
}

func SetFrontendReadFileForTest(fn func(fs.FS, string) ([]byte, error)) func() {
	previous := frontendReadFile
	frontendReadFile = fn
	return func() {
		frontendReadFile = previous
	}
}

func SetCustomStylesheetRelPathForTest(fn func(string, string) (string, error)) func() {
	previous := customStylesheetRelPathFn
	customStylesheetRelPathFn = fn
	return func() {
		customStylesheetRelPathFn = previous
	}
}
