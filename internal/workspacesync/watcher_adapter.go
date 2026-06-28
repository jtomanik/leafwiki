package workspacesync

import (
	"context"
	"sync"
	"time"

	"github.com/sgtdi/fswatcher"
)

const watcherDebounce = 250 * time.Millisecond

var workspacesyncNewFSWatcher = fswatcher.New

func newFileWatcher(rootDir string) (fileWatcher, error) {
	watcher, err := workspacesyncNewFSWatcher(
		fswatcher.WithCooldown(watcherDebounce),
		fswatcher.WithPath(
			rootDir,
			fswatcher.WithPathIncRegex(`(?i).*\.md$`),
			fswatcher.WithPathExcRegex(
				`(^|/)\.git(/|$)`,
				`(^|/)\.leafwiki(/|$)`,
				`(^|/)\.DS_Store$`,
				`\.swp$`,
				`\.tmp$`,
				`\.download$`,
				`\.partial$`,
				`\.crdownload$`,
				`~$`,
			),
		),
	)
	if err != nil {
		return nil, err
	}
	return &fsWatcherAdapter{
		watcher: watcher,
		events:  make(chan watcherEvent, 32),
		dropped: make(chan watcherEvent, 32),
	}, nil
}

type fsWatcherAdapter struct {
	watcher fswatcher.Watcher
	events  chan watcherEvent
	dropped chan watcherEvent
}

func (a *fsWatcherAdapter) Watch(ctx context.Context) error {
	pumpCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go a.pump(pumpCtx, &wg, a.watcher.Events(), a.events, false)
	go a.pump(pumpCtx, &wg, a.watcher.Dropped(), a.dropped, true)
	err := a.watcher.Watch(ctx)
	cancel()
	wg.Wait()
	close(a.events)
	close(a.dropped)
	return err
}

func (a *fsWatcherAdapter) pump(ctx context.Context, wg *sync.WaitGroup, from <-chan fswatcher.WatchEvent, to chan<- watcherEvent, dropped bool) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-from:
			if !ok {
				return
			}
			select {
			case to <- watcherEvent{Path: event.Path, Dropped: dropped}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *fsWatcherAdapter) Events() <-chan watcherEvent {
	return a.events
}

func (a *fsWatcherAdapter) Dropped() <-chan watcherEvent {
	return a.dropped
}

func (a *fsWatcherAdapter) Close() {
	a.watcher.Close()
}
