//go:build !windows

package projectdaemon

import (
	"os"
	"syscall"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeUnixDescriptorFileInfo struct {
	mode os.FileMode
	sys  any
}

func (f fakeUnixDescriptorFileInfo) Name() string       { return "project-daemon.json" }
func (f fakeUnixDescriptorFileInfo) Size() int64        { return 0 }
func (f fakeUnixDescriptorFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeUnixDescriptorFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeUnixDescriptorFileInfo) IsDir() bool        { return false }
func (f fakeUnixDescriptorFileInfo) Sys() any           { return f.sys }

var _ = ginkgo.Describe("descriptor owner unix edges", ginkgo.Label("unit"), func() {
	ginkgo.It("rejects descriptors with unavailable or mismatched owners", func() {
		unavailable := fakeUnixDescriptorFileInfo{mode: 0o600}
		Expect(validateDescriptorOwner("descriptor.json", unavailable)).To(MatchError(errDescriptorOwnerUnavailable))

		mismatched := fakeUnixDescriptorFileInfo{
			mode: 0o600,
			sys:  &syscall.Stat_t{Uid: uint32(os.Geteuid() + 1)},
		}
		Expect(validateDescriptorOwner("descriptor.json", mismatched)).To(MatchError(errDescriptorOwnerMismatch))

		originalLstatDescriptorFile := lstatDescriptorFile
		lstatDescriptorFile = func(string) (os.FileInfo, error) {
			return unavailable, nil
		}
		ginkgo.DeferCleanup(func() {
			lstatDescriptorFile = originalLstatDescriptorFile
		})

		_, err := ReadTrustedDescriptor("descriptor.json")
		Expect(err).To(MatchError(errDescriptorOwnerUnavailable))
	})
})
