package wikid

import (
	"encoding/json"
	"os"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/workspaceid"
)

func wikidTestTempDir() string {
	ginkgo.GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-wikid-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(func() {
		Expect(os.RemoveAll(dir)).To(Succeed())
	})
	return dir
}

func mustDecodeWorkspaceID(raw string) workspaceid.WorkspaceID {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var id workspaceid.WorkspaceID
	Expect(json.Unmarshal(payload, &id)).To(Succeed())
	return id
}

func mustDecodeGrantRole(raw string) GrantRole {
	ginkgo.GinkgoHelper()

	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var role GrantRole
	Expect(json.Unmarshal(payload, &role)).To(Succeed())
	return role
}
