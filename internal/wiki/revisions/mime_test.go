package revisions

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.DescribeTable("TestDetectRevisionAssetMIMETypeFallsBackToExtensionThenOctetStream",
	func(name string, manifestMIME string, want string) {
		Expect(DetectRevisionAssetMIMEType(name, manifestMIME)).To(Equal(want))
	},
	ginkgo.Entry("manifest value wins", "style.css", "text/custom", "text/custom"),
	ginkgo.Entry("extension fallback", "style.css", "", "text/css; charset=utf-8"),
	ginkgo.Entry("octet stream fallback", "asset.unknownext", "", "application/octet-stream"),
)
