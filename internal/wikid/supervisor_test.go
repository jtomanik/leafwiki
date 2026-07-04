package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
)

type daemonRestartDecision uint8

const (
	daemonRestartScheduled daemonRestartDecision = iota + 1
	daemonRestartExhausted
)

type daemonCrashReason string

const (
	daemonCrashProcessExit          daemonCrashReason = "exit status 2"
	daemonCrashInitialBudgetFailure daemonCrashReason = "first crash"
	daemonCrashFinalBudgetFailure   daemonCrashReason = "second crash"
)

func (reason daemonCrashReason) String() string {
	return string(reason)
}

type daemonCrashResult struct {
	Decision  daemonRestartDecision
	RestartAt time.Time
}

func recordDaemonCrash(supervisor *Supervisor, role projectdaemon.RoleName, message string) daemonCrashResult {
	ginkgo.GinkgoHelper()

	restartAt, ok := supervisor.RecordCrash(role, message)
	if ok {
		return daemonCrashResult{Decision: daemonRestartScheduled, RestartAt: restartAt}
	}
	return daemonCrashResult{Decision: daemonRestartExhausted}
}

var _ = ginkgo.Describe("daemon role supervision", func() {
	ginkgo.It("records crashes as bounded restarts and returns to ready after a successful restart", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		supervisor := NewSupervisor(SupervisorOptions{
			MaxRestarts: 2,
			Backoff:     time.Second,
			Now:         func() time.Time { return now },
		})
		supervisor.MarkReady(projectdaemon.RoleFrontd, 111, "http://127.0.0.1:8080", false)
		supervisor.MarkReady(projectdaemon.RoleWorkspaced, 222, "http://127.0.0.1:43111", true)

		Expect(supervisor.State(projectdaemon.RoleWorkspaced)).To(HaveField("State", Equal(projectdaemon.RoleStateReady)))

		Expect(recordDaemonCrash(supervisor, projectdaemon.RoleWorkspaced, daemonCrashProcessExit.String())).To(SatisfyAll(
			HaveField("Decision", Equal(daemonRestartScheduled)),
			HaveField("RestartAt", BeTemporally("==", now.Add(time.Second))),
		))
		Expect(supervisor.State(projectdaemon.RoleWorkspaced)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(projectdaemon.RoleStateRestarting),
			"Error": Equal(daemonCrashProcessExit.String()),
		}))

		supervisor.MarkReady(projectdaemon.RoleWorkspaced, 333, "http://127.0.0.1:43112", true)
		Expect(supervisor.State(projectdaemon.RoleWorkspaced)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(projectdaemon.RoleStateReady),
			"PID":   Equal(333),
		}))
	})

	ginkgo.It("marks the role crashed after the restart budget is exhausted", func() {
		supervisor := NewSupervisor(SupervisorOptions{MaxRestarts: 1, Backoff: time.Second})
		supervisor.MarkReady(projectdaemon.RoleWorkspaced, 222, "", true)

		Expect(recordDaemonCrash(supervisor, projectdaemon.RoleWorkspaced, daemonCrashInitialBudgetFailure.String())).To(HaveField("Decision", Equal(daemonRestartScheduled)))
		Expect(recordDaemonCrash(supervisor, projectdaemon.RoleWorkspaced, daemonCrashFinalBudgetFailure.String())).To(HaveField("Decision", Equal(daemonRestartExhausted)))
		Expect(supervisor.State(projectdaemon.RoleWorkspaced)).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"State": Equal(projectdaemon.RoleStateCrashed),
			"Error": Equal(daemonCrashFinalBudgetFailure.String()),
		}))
	})
})
