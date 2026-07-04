package revision

import (
	"errors"
	. "github.com/onsi/ginkgo/v2"
	"os"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

func setRevisionSeam[T any](target *T, replacement T) func() {
	GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	DeferCleanup(restore)
	return restore
}

func revisionIssueCodes(issues []RevisionIntegrityIssue) []sharederrors.ErrorCode {
	GinkgoHelper()

	codes := make([]sharederrors.ErrorCode, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	return codes
}

type fakeRevisionDirEntry struct {
	name string
}

func (e fakeRevisionDirEntry) Name() string {
	return e.name
}

func (fakeRevisionDirEntry) IsDir() bool {
	return false
}

func (fakeRevisionDirEntry) Type() os.FileMode {
	return 0
}

func (fakeRevisionDirEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("not implemented")
}
