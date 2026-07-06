package tree

import (
	"encoding/json"
	. "github.com/onsi/gomega"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get page raw content not serialized to JSON", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "JSON Test", newFixtureSlug("json-test"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode: %v",

			err,
		)

		page, err := svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage: %v",

			err)

		data, err := json.Marshal(page)
		Expect(err).To(Succeed(), "json.Marshal: %v",

			err)

		s := string(data)
		Expect(s).NotTo(SatisfyAny(ContainSubstring("rawContent"), ContainSubstring("raw_content")),
			"RawContent must not appear in JSON output, got: %s", s)

	})
})
