package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

type workspaceRestartDecision uint8

const (
	workspaceRestartScheduled workspaceRestartDecision = iota + 1
	workspaceRestartExhausted
)

type workspaceCrashReason string

const (
	workspaceCrashProcessExit        workspaceCrashReason = "exit status 2"
	workspaceCrashRestartBudgetFinal workspaceCrashReason = "second crash"
)

func (reason workspaceCrashReason) String() string {
	return string(reason)
}

type workspaceCrashResult struct {
	Decision  workspaceRestartDecision
	RestartAt time.Time
}

func recordWorkspaceCrash(supervisor *WorkspaceSupervisor, workspaceID workspaceid.WorkspaceID, message string) workspaceCrashResult {
	ginkgo.GinkgoHelper()

	restartAt, ok := supervisor.RecordCrash(workspaceID, message)
	if ok {
		return workspaceCrashResult{Decision: workspaceRestartScheduled, RestartAt: restartAt}
	}
	return workspaceCrashResult{Decision: workspaceRestartExhausted}
}

var _ = ginkgo.Describe("workspace process supervision", func() {
	ginkgo.It("keeps other workspaces running while restarting and then crashing a failing workspace", func() {
		now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
		supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{
			MaxRestarts: 1,
			Backoff:     time.Second,
			Now:         func() time.Time { return now },
		})
		homeWorkspaceID := workspaceid.WorkspaceID("home")
		alphaWorkspaceID := workspaceid.WorkspaceID("alpha")
		supervisor.MarkReady(homeWorkspaceID, 111, "http://127.0.0.1:41001")
		supervisor.MarkReady(alphaWorkspaceID, 222, "http://127.0.0.1:41002")

		Expect(recordWorkspaceCrash(supervisor, alphaWorkspaceID, workspaceCrashProcessExit.String())).To(SatisfyAll(
			HaveField("Decision", Equal(workspaceRestartScheduled)),
			HaveField("RestartAt", BeTemporally("==", now.Add(time.Second))),
		))

		Expect(supervisor.Status(homeWorkspaceID)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(WorkspaceStateRunning),
			"PID":   Equal(111),
		}))
		Expect(supervisor.Status(alphaWorkspaceID)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(WorkspaceStateRestarting),
			"Error": Equal(workspaceCrashProcessExit.String()),
		}))

		Expect(recordWorkspaceCrash(supervisor, alphaWorkspaceID, workspaceCrashRestartBudgetFinal.String())).To(HaveField("Decision", Equal(workspaceRestartExhausted)))
		Expect(supervisor.Status(alphaWorkspaceID)).To(HaveField("State", Equal(WorkspaceStateCrashed)))
	})
})
