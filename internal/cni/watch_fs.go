package cni

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"

	"github.com/fsnotify/fsnotify"
)

// watch combines the path, operations and event handler/function into a single
// type.
type watch struct {
	path       string
	operations []fsnotify.Op
	eventFN    func(fsnotify.Event) error
}

// applies returns true if the operation is included in the set the watch is
// looking for.
func (fsw watch) applies(op fsnotify.Op) bool {
	return slices.ContainsFunc(fsw.operations, op.Has)
}

func (fsw watch) String() string {
	return fmt.Sprintf("path=%s operations=%v", fsw.path, fsw.operations)
}

// fire is called by the event loop below and calls the event function on the
// watch.
func (fsw watch) fire(event fsnotify.Event) error {
	return fsw.eventFN(event)
}

// watchFS establishes a watch on each of the entries provided by watches. Each
// entry implements the watch interface above in order to filter fs events
// and fire appropriate events.
func (i *installer) watchFS(ctx context.Context, errs chan<- error,
	watches []watch) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	i.watcherErrors, i.watcherEvents = watcher.Errors, watcher.Events
	// build an index of path -> watch such that events with a child path
	// are processed
	index := map[string]watch{}
	begin := make(chan struct{})
	go func() {
		defer close(errs)
		<-begin
		var event fsnotify.Event
		var err error
		for {
			select {
			case <-ctx.Done():
				if err = watcher.Close(); err != nil {
					errs <- err
				}
				return
			case err = <-i.watcherErrors:
				errs <- err
			case event = <-i.watcherEvents:
				// find the watch by the event name (filesystem path)
				if watch, ok := index[event.Name]; ok && watch.applies(event.Op) {
					if err = watch.fire(event); err != nil {
						errs <- err
					}
				} else if watch, ok := index[path.Dir(event.Name)]; ok && watch.applies(event.Op) {
					if err = watch.fire(event); err != nil {
						errs <- err
					}
				}
			}
		}
	}()
	for _, watch := range watches {
		_, err := os.Stat(watch.path)
		if err != nil {
			return err
		}
		err = watcher.Add(watch.path)
		if err != nil {
			return err
		}
		index[watch.path] = watch
	}
	close(begin)
	return nil
}
