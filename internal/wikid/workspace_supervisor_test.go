package wikid

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"time"
)

var _ = ginkgo.It("TestWorkspaceSupervisorTracksIndependentWorkspaceState", func() {
	t := ginkgo.GinkgoT()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	supervisor := NewWorkspaceSupervisor(WorkspaceSupervisorOptions{
		MaxRestarts: 1,
		Backoff:     time.Second,
		Now:         func() time.Time { return now },
	})
	supervisor.MarkReady("home", 111, "http://127.0.0.1:41001")
	supervisor.MarkReady("alpha", 222, "http://127.0.0.1:41002")

	restartAt, ok := supervisor.RecordCrash("alpha", "exit status 2")
	if !ok {
		t.Fatalf("first alpha crash should schedule restart")
	}
	if !restartAt.Equal(now.Add(time.Second)) {
		t.Fatalf("restartAt = %v, want %v", restartAt, now.Add(time.Second))
	}

	home := supervisor.Status("home")
	alpha := supervisor.Status("alpha")
	if home.State != WorkspaceStateRunning || home.PID != 111 {
		t.Fatalf("home status = %#v, want still running", home)
	}
	if alpha.State != WorkspaceStateRestarting || alpha.Error != "exit status 2" {
		t.Fatalf("alpha status = %#v, want restarting with error", alpha)
	}

	if _, ok := supervisor.RecordCrash("alpha", "second crash"); ok {
		t.Fatalf("second alpha crash should exhaust restart budget")
	}
	if alpha = supervisor.Status("alpha"); alpha.State != WorkspaceStateCrashed {
		t.Fatalf("alpha after budget = %#v, want crashed", alpha)
	}
})
