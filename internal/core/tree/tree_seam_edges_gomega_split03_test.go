package tree

import (
	. "github.com/onsi/ginkgo/v2"
	"io/fs"
	"os"
	"time"
)

func swapTreeSeam[T any](target *T, replacement T) {
	GinkgoHelper()
	previous := *target
	*target = replacement
	DeferCleanup(func() {
		*target = previous
	})
}

type fakeTreeFileInfo struct {
	name    string
	mode    os.FileMode
	modTime time.Time
}

func (f fakeTreeFileInfo) Name() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}

func (f fakeTreeFileInfo) Size() int64 {
	return 0
}

func (f fakeTreeFileInfo) Mode() os.FileMode {
	return f.mode
}

func (f fakeTreeFileInfo) ModTime() time.Time {
	if f.modTime.IsZero() {
		return time.Date(2026, time.June, 27, 12, 0, 0, 0, time.UTC)
	}
	return f.modTime
}

func (f fakeTreeFileInfo) IsDir() bool {
	return f.mode.IsDir()
}

func (f fakeTreeFileInfo) Sys() any {
	return nil
}

type fakeTreeDirEntry struct {
	name    string
	isDir   bool
	mode    os.FileMode
	infoErr error
}

func (e fakeTreeDirEntry) Name() string {
	return e.name
}

func (e fakeTreeDirEntry) IsDir() bool {
	return e.isDir
}

func (e fakeTreeDirEntry) Type() os.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	if e.mode != 0 {
		return e.mode
	}
	return 0
}

func (e fakeTreeDirEntry) Info() (os.FileInfo, error) {
	if e.infoErr != nil {
		return nil, e.infoErr
	}
	return fakeTreeFileInfo{name: e.name, mode: e.Type()}, nil
}
