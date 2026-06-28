package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"time"

	"github.com/perber/wiki/internal/projectdaemon"
)

var _ = ginkgo.It("TestSupervisorRecordsCrashAndSchedulesBoundedRestart", func() {
	t := ginkgo.GinkgoT()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	supervisor := NewSupervisor(SupervisorOptions{
		MaxRestarts: 2,
		Backoff:     time.Second,
		Now:         func() time.Time { return now },
	})
	supervisor.MarkReady(projectdaemon.RoleFrontd, 111, "http://127.0.0.1:8080", false)
	supervisor.MarkReady(projectdaemon.RoleWorkspaced, 222, "http://127.0.0.1:43111", true)

	if state := supervisor.State(projectdaemon.RoleWorkspaced); state.State != projectdaemon.RoleStateReady {
		t.Fatalf("initial workspaced state = %#v, want ready", state)
	}

	restartAt, ok := supervisor.RecordCrash(projectdaemon.RoleWorkspaced, "exit status 2")
	if !ok {
		t.Fatalf("first crash did not schedule restart")
	}
	if !restartAt.Equal(now.Add(time.Second)) {
		t.Fatalf("restartAt = %v, want %v", restartAt, now.Add(time.Second))
	}
	if state := supervisor.State(projectdaemon.RoleWorkspaced); state.State != projectdaemon.RoleStateRestarting || state.Error != "exit status 2" {
		t.Fatalf("crashed workspaced state = %#v, want restarting with error", state)
	}

	supervisor.MarkReady(projectdaemon.RoleWorkspaced, 333, "http://127.0.0.1:43112", true)
	if state := supervisor.State(projectdaemon.RoleWorkspaced); state.State != projectdaemon.RoleStateReady || state.PID != 333 {
		t.Fatalf("restarted workspaced state = %#v", state)
	}
})

var _ = ginkgo.It("TestSupervisorStopsRestartingAfterBudgetIsExhausted", func() {
	t := ginkgo.GinkgoT()
	supervisor := NewSupervisor(SupervisorOptions{MaxRestarts: 1, Backoff: time.Second})
	supervisor.MarkReady(projectdaemon.RoleWorkspaced, 222, "", true)

	if _, ok := supervisor.RecordCrash(projectdaemon.RoleWorkspaced, "first crash"); !ok {
		t.Fatalf("first crash should schedule restart")
	}
	if _, ok := supervisor.RecordCrash(projectdaemon.RoleWorkspaced, "second crash"); ok {
		t.Fatalf("second crash unexpectedly scheduled restart after budget exhausted")
	}
	if state := supervisor.State(projectdaemon.RoleWorkspaced); state.State != projectdaemon.RoleStateCrashed || state.Error != "second crash" {
		t.Fatalf("exhausted state = %#v, want crashed with latest error", state)
	}
})
