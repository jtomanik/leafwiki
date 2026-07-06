package frontd

import (
	"encoding/json"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/workspaceid"
)

func mustDecodeWorkspaceID(raw string) workspaceid.WorkspaceID {
	payload, err := json.Marshal(raw)
	Expect(err).To(Succeed())
	var id workspaceid.WorkspaceID
	Expect(json.Unmarshal(payload, &id)).To(Succeed())
	return id
}

func matchFrontdActorSubjectID(subjectID string) types.GomegaMatcher {
	return WithTransform(func(ctx projectdaemon.ActorContext) string {
		return ctx.SubjectID()
	}, Equal(subjectID))
}
