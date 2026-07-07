package workspaced

import (
	"encoding/json"

	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/workspaceid"
)

func mustDecodeWorkspaceID(raw string) workspaceid.WorkspaceID {
	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var id workspaceid.WorkspaceID
	Expect(json.Unmarshal(payload, &id)).To(Succeed())
	return id
}
