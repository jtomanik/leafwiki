package wikid

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("wikid document and supervisor values", ginkgo.Label("unit"), func() {
	ginkgo.It("parses grant roles and validates grant documents", func() {
		role, err := ParseGrantRole(" editor ")
		Expect(err).To(Succeed())
		Expect(role).To(Equal(GrantRoleEditor))

		_, err = ParseGrantRole("owner")
		Expect(err).To(MatchError(ErrUnknownGrantRole))
		Expect(grantRoleValidityFor(GrantRoleAdmin)).To(Equal(grantRoleAccepted))
		Expect(grantRoleValidityFor(mustDecodeGrantRole("owner"))).To(Equal(grantRoleRejected))

		document := NewGrantDocument()
		document.Grants = []Grant{{
			Subject:     "user:admin",
			WorkspaceID: HomeWorkspaceID,
			Role:        GrantRoleAdmin,
		}}

		Expect(document.Validate()).To(Succeed())
	})

	ginkgo.It("reports registry lookup and validation state without mutating workspace records", func() {
		now := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
		home := WorkspaceRecord{
			ID:          HomeWorkspaceID,
			DisplayName: "Home",
			DataDir:     "/data/home",
			RootDir:     "/root/home",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		registry := NewRegistryDocument()
		registry.Workspaces = []WorkspaceRecord{home}

		Expect(registry.Validate()).To(Succeed())
		Expect(registryLookupFor(registry, HomeWorkspaceID)).To(Equal(registryLookup{
			Outcome: recordFound,
			Record:  home,
		}))
		Expect(registryLookupFor(registry, mustDecodeWorkspaceID("docs"))).To(Equal(registryLookup{
			Outcome: recordMissing,
		}))
	})

	ginkgo.It("keeps empty workspace statuses out of supervisor snapshots", func() {
		now := time.Date(2026, 7, 7, 9, 30, 0, 0, time.UTC)
		supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{
			Now: func() time.Time {
				return now
			},
		})
		var emptyWorkspaceID workspaceid.WorkspaceID

		supervisor.MarkStatus(WorkspaceStatus{
			WorkspaceID: emptyWorkspaceID,
			State:       WorkspaceStateRunning,
			PID:         1234,
		})

		Expect(supervisor.Status(HomeWorkspaceID)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID": Equal(HomeWorkspaceID),
			"State":       Equal(WorkspaceStateRegistered),
		}))
		Expect(supervisor.Statuses()).To(BeEmpty())

		supervisor.MarkStatus(WorkspaceStatus{
			WorkspaceID: HomeWorkspaceID,
			State:       WorkspaceStateRunning,
			PID:         1234,
			URL:         "http://127.0.0.1:43111",
		})

		Expect(supervisor.Status(HomeWorkspaceID)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State":     Equal(WorkspaceStateRunning),
			"UpdatedAt": Equal(now),
		}))
	})
})

type grantRoleValidity uint8

const (
	grantRoleRejected grantRoleValidity = iota
	grantRoleAccepted
)

func grantRoleValidityFor(role GrantRole) grantRoleValidity {
	if role.Valid() {
		return grantRoleAccepted
	}
	return grantRoleRejected
}

type registryLookupOutcome uint8

const (
	recordMissing registryLookupOutcome = iota
	recordFound
)

type registryLookup struct {
	Outcome registryLookupOutcome
	Record  WorkspaceRecord
}

func registryLookupFor(registry RegistryDocument, workspaceID workspaceid.WorkspaceID) registryLookup {
	record, ok := registry.Workspace(workspaceID)
	if ok {
		return registryLookup{Outcome: recordFound, Record: record}
	}
	return registryLookup{Outcome: recordMissing}
}
