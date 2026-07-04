package workspacesync

import (
	"context"
	"sync"
	"time"
)

func (s *Service) StartWatcher(ctx context.Context) error {
	if !s.enabled {
		return nil
	}
	factory := s.watcherFactory
	if factory == nil {
		factory = newFileWatcher
	}
	watcher, err := factory(s.rootDir)
	if err != nil {
		s.mu.Lock()
		s.status.WatcherEnabled = true
		s.status.WatcherRunning = false
		s.status.LastError = err.Error()
		s.mu.Unlock()
		return err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.mu.Lock()
	s.status.WatcherEnabled = true
	s.status.WatcherRunning = true
	s.watcher = watcher
	s.watcherCancel = cancel
	s.watcherDone = done
	s.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := watcher.Watch(watchCtx); err != nil && watchCtx.Err() == nil {
			s.mu.Lock()
			s.status.LastError = err.Error()
			s.mu.Unlock()
			if _, syncErr := s.SyncNow(context.Background(), SyncRequest{
				Reason: ReasonWatcher,
				Source: SourceFilesystem,
				Actor:  PublicEditorActor(),
			}); syncErr != nil {
				s.mu.Lock()
				s.status.LastError = err.Error() + ": " + syncErr.Error()
				s.mu.Unlock()
			} else {
				s.mu.Lock()
				s.status.LastError = err.Error()
				s.mu.Unlock()
			}
		}
		s.mu.Lock()
		if s.watcher == watcher {
			s.watcher = nil
		}
		s.status.WatcherRunning = false
		s.mu.Unlock()
	}()
	go func() {
		defer wg.Done()
		s.consumeWatcherEvents(watchCtx, watcher)
	}()
	go func() {
		wg.Wait()
		close(done)
	}()
	return nil
}

func (s *Service) StopWatcher() {
	s.mu.Lock()
	cancel := s.watcherCancel
	watcher := s.watcher
	done := s.watcherDone
	s.watcher = nil
	s.watcherCancel = nil
	s.watcherDone = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closer, ok := watcher.(closableFileWatcher); ok {
		closer.Close()
	}
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (s *Service) consumeWatcherEvents(ctx context.Context, watcher fileWatcher) {
	var timer *time.Timer
	var timerC <-chan time.Time
	pendingCount := 0
	dropped := false
	droppedPath := ""

	stopTimer := func() {
		if timer == nil {
			return
		}
		stopWorkspacesyncTimer(timer)
		timer = nil
		timerC = nil
	}
	queue := func(event watcherEvent) {
		relPath, managed := managedMarkdownEventPath(s.rootDir, event.Path)
		if !managed && !event.Dropped {
			return
		}
		pendingCount++
		if event.Dropped {
			dropped = true
			if relPath != "" {
				droppedPath = relPath
			}
		}
		s.mu.Lock()
		s.status.PendingEventCount++
		s.mu.Unlock()
		if timer == nil {
			timer = time.NewTimer(watcherBatchDebounce)
			timerC = timer.C
			return
		}
		stopWorkspacesyncTimer(timer)
		timer.Reset(watcherBatchDebounce)
	}
	flush := func() {
		if pendingCount == 0 {
			return
		}
		stopTimer()
		s.handleWatcherBatch(ctx, pendingCount, dropped, droppedPath)
		pendingCount = 0
		dropped = false
		droppedPath = ""
	}

	defer stopTimer()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timerC:
			flush()
		case event, ok := <-watcher.Events():
			if !ok {
				flush()
				return
			}
			queue(event)
		case event, ok := <-watcher.Dropped():
			if !ok {
				flush()
				return
			}
			event.Dropped = true
			queue(event)
		}
	}
}

func stopWorkspacesyncTimer(timer *time.Timer) {
	if !timer.Stop() {
		drainWorkspacesyncTimer(timer)
	}
}

func drainWorkspacesyncTimer(timer *time.Timer) {
	select {
	case <-timer.C:
	default:
	}
}

func (s *Service) handleWatcherBatch(ctx context.Context, eventCount int, dropped bool, droppedPath string) {
	_, err := s.SyncNow(ctx, SyncRequest{
		Reason: ReasonWatcher,
		Source: SourceFilesystem,
		Actor:  PublicEditorActor(),
	})

	s.mu.Lock()
	s.status.PendingEventCount -= eventCount
	if s.status.PendingEventCount < 0 {
		s.status.PendingEventCount = 0
	}
	if dropped {
		s.status.LastError = watcherDroppedEventsStatus(droppedPath)
	} else if err != nil {
		s.status.LastError = err.Error()
	}
	s.mu.Unlock()
}

func watcherDroppedEventsStatus(droppedPath string) string {
	if droppedPath == "" {
		return watcherDroppedEventsStatusLabel
	}
	return watcherDroppedEventsStatusLabel + " for " + droppedPath
}

func (s *Service) shouldLogStartupSync(req SyncRequest) bool {
	return req.Reason == ReasonStartup && s.log != nil
}

func (s *Service) logStartupSyncStarted(enabled bool, req SyncRequest) time.Time {
	if !enabled {
		return time.Time{}
	}
	started := time.Now()
	s.log.Info("workspace sync startup started",
		"reason", string(req.Reason),
		"source", string(req.Source),
		"root_dir", s.rootDir,
	)
	return started
}

func (s *Service) logStartupSyncCompleted(enabled bool, started time.Time, status SyncStatus) {
	if !enabled {
		return
	}
	s.log.Info("workspace sync startup completed",
		"duration", time.Since(started),
		"last_commit_hash", status.LastCommitHash,
		"last_error", status.LastError,
		"validation_errors", len(status.ValidationErrors),
	)
}

func (s *Service) logStartupSyncFailed(enabled bool, started time.Time, err error) {
	if !enabled {
		return
	}
	s.log.Error("workspace sync startup failed",
		"duration", time.Since(started),
		"error", err,
	)
}

func (s *Service) logStartupPhaseStarted(enabled bool, phase string, attrs ...any) time.Time {
	if !enabled {
		return time.Time{}
	}
	started := time.Now()
	args := append([]any{"phase", phase}, attrs...)
	s.log.Info("workspace sync startup phase started", args...)
	return started
}

func (s *Service) logStartupPhaseCompleted(enabled bool, phase string, started time.Time, attrs ...any) {
	if !enabled {
		return
	}
	args := append([]any{
		"phase", phase,
		"duration", time.Since(started),
	}, attrs...)
	s.log.Info("workspace sync startup phase completed", args...)
}

func (s *Service) logStartupPhaseFailed(enabled bool, phase string, started time.Time, err error) {
	if !enabled {
		return
	}
	s.log.Error("workspace sync startup phase failed",
		"phase", phase,
		"duration", time.Since(started),
		"error", err,
	)
}
