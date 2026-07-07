package wikid

import (
	"errors"
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

	ginkgo.DescribeTable("grant role parsing",
		func(raw string, want parsedGrantRole) {
			Expect(parseGrantRoleFor(raw)).To(Equal(want))
		},
		ginkgo.Entry("accepts viewer roles", "viewer", parsedGrantRole{
			Outcome: grantRoleParsed,
			Role:    GrantRoleViewer,
		}),
		ginkgo.Entry("accepts administrator roles", "admin", parsedGrantRole{
			Outcome: grantRoleParsed,
			Role:    GrantRoleAdmin,
		}),
		ginkgo.Entry("rejects empty roles", " ", parsedGrantRole{
			Outcome: grantRoleParseRejected,
		}),
	)

	ginkgo.DescribeTable("grant document validation failures",
		func(document GrantDocument, want grantDocumentValidation) {
			Expect(grantDocumentValidationFor(document)).To(Equal(want))
		},
		ginkgo.Entry("rejects incompatible schema versions", GrantDocument{SchemaVersion: 0}, grantDocumentSchemaRejected),
		ginkgo.Entry("rejects empty grant subjects", GrantDocument{
			SchemaVersion: GrantSchemaVersion,
			Grants: []Grant{{
				WorkspaceID: HomeWorkspaceID,
				Role:        GrantRoleViewer,
			}},
		}, grantDocumentSubjectRejected),
		ginkgo.Entry("rejects unknown grant roles", GrantDocument{
			SchemaVersion: GrantSchemaVersion,
			Grants: []Grant{{
				Subject:     "user:admin",
				WorkspaceID: HomeWorkspaceID,
				Role:        mustDecodeGrantRole("owner"),
			}},
		}, grantDocumentRoleRejected),
	)

	ginkgo.It("rejects capability lookups for unknown grant roles", func() {
		Expect(capabilityLookupFor(mustDecodeGrantRole("owner"))).To(Equal(capabilityLookup{
			Outcome: capabilityLookupRejected,
		}))
	})

	ginkgo.DescribeTable("grant capability lookup",
		func(role GrantRole, want capabilityLookup) {
			Expect(capabilityLookupFor(role)).To(Equal(want))
		},
		ginkgo.Entry("allows viewers to read content", GrantRoleViewer, capabilityLookup{
			Outcome:      capabilityLookupAccepted,
			Capabilities: grantCapabilityReadContent,
		}),
		ginkgo.Entry("allows editors to read and write content", GrantRoleEditor, capabilityLookup{
			Outcome:      capabilityLookupAccepted,
			Capabilities: grantCapabilityReadContent | grantCapabilityWriteContent,
		}),
		ginkgo.Entry("allows administrators to manage grants", GrantRoleAdmin, capabilityLookup{
			Outcome:      capabilityLookupAccepted,
			Capabilities: grantCapabilityReadContent | grantCapabilityWriteContent | grantCapabilityAdministerGrants,
		}),
	)

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

	ginkgo.DescribeTable("workspace storage location equivalence",
		func(left WorkspaceRecord, right WorkspaceRecord, want workspaceStorageLocationRelation) {
			Expect(workspaceStorageLocationRelationFor(left, right)).To(Equal(want))
		},
		ginkgo.Entry(
			"matches data and root paths after cleaning path segments",
			workspaceStorageLocationRecord("data/home/.", "root/home/sub/.."),
			workspaceStorageLocationRecord("data/home", "root/home"),
			workspaceStorageLocationSame,
		),
		ginkgo.Entry(
			"keeps different data directories distinct",
			workspaceStorageLocationRecord("data/home", "root/home"),
			workspaceStorageLocationRecord("data/docs", "root/home"),
			workspaceStorageLocationDistinct,
		),
		ginkgo.Entry(
			"keeps different root directories distinct",
			workspaceStorageLocationRecord("data/home", "root/home"),
			workspaceStorageLocationRecord("data/home", "root/docs"),
			workspaceStorageLocationDistinct,
		),
	)

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

type workspaceStorageLocationRelation uint8

const (
	workspaceStorageLocationDistinct workspaceStorageLocationRelation = iota
	workspaceStorageLocationSame
)

func workspaceStorageLocationRecord(dataDir string, rootDir string) WorkspaceRecord {
	return WorkspaceRecord{DataDir: dataDir, RootDir: rootDir}
}

func workspaceStorageLocationRelationFor(left WorkspaceRecord, right WorkspaceRecord) workspaceStorageLocationRelation {
	if sameWorkspaceLocation(left, right) {
		return workspaceStorageLocationSame
	}
	return workspaceStorageLocationDistinct
}

type grantRoleParseOutcome uint8

const (
	grantRoleParseRejected grantRoleParseOutcome = iota
	grantRoleParsed
)

type parsedGrantRole struct {
	Outcome grantRoleParseOutcome
	Role    GrantRole
}

func parseGrantRoleFor(raw string) parsedGrantRole {
	role, err := ParseGrantRole(raw)
	if err != nil {
		return parsedGrantRole{Outcome: grantRoleParseRejected}
	}
	return parsedGrantRole{Outcome: grantRoleParsed, Role: role}
}

type grantDocumentValidation uint8

const (
	grantDocumentAccepted grantDocumentValidation = iota
	grantDocumentSchemaRejected
	grantDocumentSubjectRejected
	grantDocumentRoleRejected
)

func grantDocumentValidationFor(document GrantDocument) grantDocumentValidation {
	err := document.Validate()
	switch {
	case err == nil:
		return grantDocumentAccepted
	case errors.Is(err, ErrGrantSchemaVersion):
		return grantDocumentSchemaRejected
	case errors.Is(err, ErrGrantSubjectRequired):
		return grantDocumentSubjectRejected
	case errors.Is(err, ErrUnknownGrantRole):
		return grantDocumentRoleRejected
	default:
		return 0
	}
}

type capabilityLookupOutcome uint8

const (
	capabilityLookupRejected capabilityLookupOutcome = iota
	capabilityLookupAccepted
)

type capabilityLookup struct {
	Outcome      capabilityLookupOutcome
	Capabilities grantCapabilitySet
}

func capabilityLookupFor(role GrantRole) capabilityLookup {
	capabilities, err := CapabilitiesForRole(role)
	if err != nil {
		return capabilityLookup{Outcome: capabilityLookupRejected}
	}
	return capabilityLookup{Outcome: capabilityLookupAccepted, Capabilities: grantCapabilitySetFor(capabilities)}
}

type grantCapabilitySet uint8

const (
	grantCapabilityReadContent grantCapabilitySet = 1 << iota
	grantCapabilityWriteContent
	grantCapabilityAdministerGrants
)

func grantCapabilitySetFor(capabilities RoleCapabilities) grantCapabilitySet {
	var set grantCapabilitySet
	if capabilities.ReadContent {
		set |= grantCapabilityReadContent
	}
	if capabilities.WriteContent {
		set |= grantCapabilityWriteContent
	}
	if capabilities.AdministerGrants {
		set |= grantCapabilityAdministerGrants
	}
	return set
}
