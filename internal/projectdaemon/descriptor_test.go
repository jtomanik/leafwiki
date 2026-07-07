package projectdaemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = ginkgo.Describe("project daemon descriptors", func() {
	ginkgo.It("writes private descriptors atomically with 0600 permissions and no session secret material", ginkgo.Label("unit"), func() {
		dataDir := tempProjectdaemonDir()
		rootDir := filepath.Join(tempProjectdaemonDir(), "root")
		desc := &Descriptor{
			SchemaVersion:    DescriptorSchemaVersion,
			PID:              1234,
			StartedAt:        time.Now().UTC(),
			DataDir:          dataDir,
			RootDir:          rootDir,
			PublicURL:        "http://127.0.0.1:8080",
			PublicMCPEnabled: false,
			BasePath:         "",
			ControlURL:       "http://127.0.0.1:12345",
			ConfigHash:       "hash",
			IdleTimeout:      "10m0s",
			ControlToken:     "control-token",
			Config: Config{
				DataDir:      dataDir,
				RootDir:      rootDir,
				AuthDisabled: false,
			},
		}
		path := DescriptorPath(dataDir)

		Expect(WriteDescriptorAtomic(path, desc)).To(Succeed())

		info, err := os.Stat(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
		Expect(descriptorWireMap(path)).NotTo(SatisfyAny(
			HaveKey("jwtSecretHash"),
			HaveKey("adminPasswordHash"),
			HaveValue(Equal("lwk_secret")),
			HaveValue(Equal("jwt-secret")),
			HaveValue(Equal("admin-password")),
			HaveValue(Equal(HashSecret("jwt-secret"))),
			HaveValue(Equal(HashSecret("admin-password"))),
		))
		loaded, err := ReadDescriptor(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ControlToken": Equal("control-token"),
			"Config":       matchAuthenticatedProjectConfig(dataDir, rootDir),
		})))
	})

	ginkgo.It("round-trips runtime stack and role health metadata", ginkgo.Label("unit"), func() {
		dataDir := tempProjectdaemonDir()
		rootDir := filepath.Join(tempProjectdaemonDir(), "root")
		now := time.Now().UTC().Truncate(time.Second)
		desc := &Descriptor{
			SchemaVersion:    DescriptorSchemaVersion,
			RuntimeStack:     RuntimeStackWikidFrontd,
			Role:             RoleWikid,
			PID:              1234,
			StartedAt:        now,
			DataDir:          dataDir,
			RootDir:          rootDir,
			PublicURL:        "http://127.0.0.1:8080",
			PublicMCPEnabled: true,
			BasePath:         "",
			ControlURL:       "http://127.0.0.1:12345",
			ConfigHash:       "hash",
			IdleTimeout:      "10m0s",
			ControlToken:     "control-token",
			Config: Config{
				DataDir:      dataDir,
				RootDir:      rootDir,
				AuthDisabled: true,
				RuntimeStack: RuntimeStackWikidFrontd,
			},
			Roles: []RoleHealth{
				{Name: RoleWikid, State: RoleStateReady, PID: 1234, UpdatedAt: now},
				{Name: RoleFrontd, State: RoleStateReady, PID: 2345, URL: "http://127.0.0.1:8080", UpdatedAt: now},
				{Name: RoleWorkspaced, State: RoleStateReady, PID: 3456, URL: "http://127.0.0.1:43111", Private: true, UpdatedAt: now},
			},
		}
		path := DescriptorPath(dataDir)

		Expect(WriteDescriptorAtomic(path, desc)).To(Succeed())

		rawDescriptor, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		var wire struct {
			RuntimeStack string            `json:"runtimeStack"`
			Role         RoleName          `json:"role"`
			Roles        []json.RawMessage `json:"roles"`
		}
		Expect(json.Unmarshal(rawDescriptor, &wire)).To(Succeed())
		Expect(wire).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RuntimeStack": Equal(RuntimeStackWikidFrontd),
			"Role":         Equal(RoleWikid),
			"Roles":        Not(BeEmpty()),
		}))
		Expect(descriptorWireMap(path)).NotTo(HaveValue(BeEquivalentTo("session-api-key")))
		loaded, err := ReadTrustedDescriptor(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"RuntimeStack": Equal(RuntimeStackWikidFrontd),
			"Role":         Equal(RoleWikid),
			"Roles":        ContainElement(matchPrivateReadyWorkspacedRoleHealth(now)),
		})))
	})

	ginkgo.It("round-trips workspace identity and private MCP attach metadata without leaking tokens in mismatch messages", ginkgo.Label("unit"), func() {
		dataDir := tempProjectdaemonDir()
		rootDir := filepath.Join(tempProjectdaemonDir(), "root")
		desc := &Descriptor{
			SchemaVersion:    DescriptorSchemaVersion,
			RuntimeStack:     RuntimeStackWikidFrontd,
			Role:             RoleWorkspaced,
			WorkspaceID:      mustDecodeWorkspaceID("home"),
			PID:              1234,
			StartedAt:        time.Now().UTC().Truncate(time.Second),
			DataDir:          dataDir,
			RootDir:          rootDir,
			PublicURL:        "http://127.0.0.1:8080",
			PublicMCPEnabled: true,
			BasePath:         "",
			ControlURL:       "http://127.0.0.1:12345",
			PrivateMCPURL:    "http://127.0.0.1:23456/mcp",
			PrivateMCPToken:  "super-secret-token",
			ConfigHash:       "hash",
			IdleTimeout:      "10m0s",
			ControlToken:     "control-token",
			Config: Config{
				RuntimeStack:    RuntimeStackWikidFrontd,
				WorkspaceID:     mustDecodeWorkspaceID("home"),
				DataDir:         dataDir,
				RootDir:         rootDir,
				PrivateMCPURL:   "http://127.0.0.1:23456/mcp",
				PrivateMCPToken: "super-secret-token",
			},
		}
		path := DescriptorPath(dataDir)

		Expect(WriteDescriptorAtomic(path, desc)).To(Succeed())

		loaded, err := ReadTrustedDescriptor(path)
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"WorkspaceID":     Equal(desc.WorkspaceID),
			"PrivateMCPURL":   Equal(desc.PrivateMCPURL),
			"PrivateMCPToken": Equal(desc.PrivateMCPToken),
			"Config": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"WorkspaceID": Equal(desc.WorkspaceID),
			}),
		})))

		requested := loaded.Config
		requested.PrivateMCPToken = "different-secret"
		Expect(CompareConfig(loaded.Config, requested)).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Field": Equal("private-mcp-token"),
			"Want":  Equal("redacted"),
			"Got":   Equal("redacted"),
		})))
	})

	ginkgo.It("persists hashed config identity in role-scoped descriptors", ginkgo.Label("unit"), func() {
		runtimeDir := filepath.Join(tempProjectdaemonDir(), ".leafwiki", "runtime")
		dataDir := tempProjectdaemonDir()
		rootDir := filepath.Join(tempProjectdaemonDir(), "root")
		cfg := Config{
			RuntimeStack:    RuntimeStackWikidFrontd,
			DataDir:         dataDir,
			RootDir:         rootDir,
			PrivateMCPToken: "owner-secret",
			Port:            "8080",
		}
		configHash, err := ConfigHash(cfg)
		Expect(err).To(Succeed())
		desc := &Descriptor{
			SchemaVersion: DescriptorSchemaVersion,
			RuntimeStack:  RuntimeStackWikidFrontd,
			Role:          RoleFrontd,
			PID:           2345,
			StartedAt:     time.Now().UTC().Truncate(time.Second),
			DataDir:       dataDir,
			RootDir:       rootDir,
			ControlURL:    "http://127.0.0.1:12345",
			ConfigHash:    configHash,
			ControlToken:  "control-token",
			Config:        cfg,
		}
		path := GlobalDescriptorPath(runtimeDir, RoleFrontd)

		Expect(WriteDescriptorAtomic(path, desc)).To(Succeed())

		loaded, err := ReadTrustedDescriptor(path)
		Expect(err).To(Succeed())
		Expect(loaded).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Role":       Equal(RoleFrontd),
			"ConfigHash": SatisfyAll(HaveLen(64), Equal(configHash)),
			"Config": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"RuntimeStack":    Equal(RuntimeStackWikidFrontd),
				"PrivateMCPToken": Equal("owner-secret"),
			}),
		})))

		requested := loaded.Config
		requested.Port = "8081"
		mismatch := NewConfigMismatchError(CompareConfig(loaded.Config, requested))
		Expect(mismatch).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Mismatches": ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("port"),
				"Want":  Equal("8080"),
				"Got":   Equal("8081"),
			})),
		})))
	})

	ginkgo.DescribeTable("global descriptor paths for daemon roles",
		ginkgo.Label("unit"),
		func(roleFixture descriptorRoleFixture, want descriptorPathBaseName) {
			runtimeDir := filepath.Join(tempProjectdaemonDir(), ".leafwiki", "runtime")
			role, err := roleFixture.roleName()

			Expect(err).To(Succeed())
			Expect(globalDescriptorPathBaseNameFor(runtimeDir, role)).To(Equal(want))
		},
		ginkgo.Entry("uses the wikid role descriptor filename", descriptorRoleWikid, descriptorPathBaseWikid),
		ginkgo.Entry("uses the frontd role descriptor filename", descriptorRoleFrontd, descriptorPathBaseFrontd),
		ginkgo.Entry("uses the workspaced role descriptor filename", descriptorRoleWorkspaced, descriptorPathBaseWorkspaced),
		ginkgo.Entry("uses a fallback descriptor filename for unknown roles", descriptorRoleSidecar, descriptorPathBaseUnknownRole),
	)

	ginkgo.It("removes missing and existing descriptors idempotently", ginkgo.Label("unit"), func() {
		path := DescriptorPath(tempProjectdaemonDir())

		Expect(RemoveDescriptor(path)).To(Succeed())
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o600)).To(Succeed())
		Expect(RemoveDescriptor(path)).To(Succeed())
		_, err := os.Stat(path)
		Expect(err).To(MatchError(os.ErrNotExist))
	})

	ginkgo.It("produces deterministic hashes that change with canonical config values", ginkgo.Label("unit"), func() {
		base := Config{DataDir: "/data", RootDir: "/root", Port: "8080"}

		first, err := ConfigHash(base)
		Expect(err).NotTo(HaveOccurred())
		second, err := ConfigHash(base)
		Expect(err).NotTo(HaveOccurred())
		changed, err := ConfigHash(Config{DataDir: "/data", RootDir: "/root", Port: "8081"})
		Expect(err).NotTo(HaveOccurred())

		Expect(first).To(SatisfyAll(HaveLen(64), Equal(second)))
		Expect(changed).NotTo(Equal(first))
	})

	ginkgo.It("rejects trusted descriptors that are readable outside the owner", ginkgo.Label("unit"), func() {
		dataDir := tempProjectdaemonDir()
		path := DescriptorPath(dataDir)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte(`{"schemaVersion":1}`), 0o644)).To(Succeed())

		_, err := ReadTrustedDescriptor(path)

		Expect(err).To(MatchError(errDescriptorModeMismatch))
	})

	ginkgo.It("reports config mismatches with raw secret values redacted", ginkgo.Label("unit"), func() {
		owner := Config{
			DataDir:                "/data",
			RootDir:                "/root",
			Port:                   "8080",
			InjectCodeInHeaderHash: HashSecret("owner-code"),
		}
		requested := owner
		requested.Port = "8081"
		requested.InjectCodeInHeaderHash = HashSecret("requested-code")

		Expect(CompareConfig(owner, requested)).To(ConsistOf(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("port"),
				"Want":  Equal("8080"),
				"Got":   Equal("8081"),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Field": Equal("inject-code-in-header-hash"),
				"Want":  Equal("redacted"),
				"Got":   Equal("redacted"),
			}),
		))
	})
})

func descriptorWireMap(path string) map[string]any {
	ginkgo.GinkgoHelper()

	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	var wire map[string]any
	Expect(json.Unmarshal(raw, &wire)).To(Succeed())
	return wire
}

func matchAuthenticatedProjectConfig(dataDir string, rootDir string) types.GomegaMatcher {
	return Equal(Config{
		DataDir:      dataDir,
		RootDir:      rootDir,
		AuthDisabled: false,
	})
}

func matchPrivateReadyWorkspacedRoleHealth(updatedAt time.Time) types.GomegaMatcher {
	return Equal(RoleHealth{
		Name:      RoleWorkspaced,
		State:     RoleStateReady,
		PID:       3456,
		URL:       "http://127.0.0.1:43111",
		Private:   true,
		UpdatedAt: updatedAt,
	})
}

type descriptorRoleFixture uint8

const (
	descriptorRoleWikid descriptorRoleFixture = iota + 1
	descriptorRoleFrontd
	descriptorRoleWorkspaced
	descriptorRoleSidecar
)

func (fixture descriptorRoleFixture) roleName() (RoleName, error) {
	switch fixture {
	case descriptorRoleWikid:
		return RoleWikid, nil
	case descriptorRoleFrontd:
		return RoleFrontd, nil
	case descriptorRoleWorkspaced:
		return RoleWorkspaced, nil
	case descriptorRoleSidecar:
		var wire struct {
			Role RoleName `json:"role"`
		}
		if err := json.Unmarshal([]byte(`{"role":"sidecar"}`), &wire); err != nil {
			var role RoleName
			return role, err
		}
		return wire.Role, nil
	default:
		var role RoleName
		return role, nil
	}
}

type descriptorPathBaseName uint8

const (
	descriptorPathBaseUnexpected descriptorPathBaseName = iota
	descriptorPathBaseWikid
	descriptorPathBaseFrontd
	descriptorPathBaseWorkspaced
	descriptorPathBaseUnknownRole
)

func globalDescriptorPathBaseNameFor(runtimeDir string, role RoleName) descriptorPathBaseName {
	path := GlobalDescriptorPath(runtimeDir, role)
	switch filepath.Base(path) {
	case "wikid.json":
		return descriptorPathBaseWikid
	case "frontd.json":
		return descriptorPathBaseFrontd
	case "workspaced.json":
		return descriptorPathBaseWorkspaced
	case "unknown-role.json":
		return descriptorPathBaseUnknownRole
	default:
		return descriptorPathBaseUnexpected
	}
}
