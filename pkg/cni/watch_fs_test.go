package cni

import (
	"context"
	"os"
	"path"
	"reflect"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestWatch(t *testing.T) {
	type test struct {
		name       string
		event      fsnotify.Event
		expApplies bool
		expErr     string
		watch      watch
	}
	tests := []test{
		{
			name:       "DoesNotApplyEmpty",
			expApplies: false,
			expErr:     "",
			event:      fsnotify.Event{Name: "new-file", Op: fsnotify.Create},
			watch: watch{
				operations: []fsnotify.Op{},
				path:       "new-file",
				eventFN:    func(_ fsnotify.Event) error { return nil },
			},
		},
		{
			name:       "DoesNotApply",
			expApplies: false,
			expErr:     "",
			event:      fsnotify.Event{Name: "new-file", Op: fsnotify.Create},
			watch: watch{
				operations: []fsnotify.Op{fsnotify.Chmod, fsnotify.Remove,
					fsnotify.Rename, fsnotify.Write},
				path:    "new-file",
				eventFN: func(_ fsnotify.Event) error { return nil },
			},
		},
		{
			name:       "DoesApply",
			expApplies: true,
			expErr:     "",
			event:      fsnotify.Event{Name: "new-file", Op: fsnotify.Create},
			watch: watch{
				operations: []fsnotify.Op{fsnotify.Create},
				path:       "new-file",
				eventFN:    func(_ fsnotify.Event) error { return nil },
			},
		},
		{
			name:       "DoesApplyMultiple",
			expApplies: true,
			expErr:     "",
			event:      fsnotify.Event{Name: "new-file", Op: fsnotify.Create},
			watch: watch{
				operations: []fsnotify.Op{fsnotify.Chmod, fsnotify.Create},
				path:       "new-file",
				eventFN:    func(_ fsnotify.Event) error { return nil },
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			applies := test.watch.applies(test.event.Op)
			if applies != test.expApplies {
				t.Fatalf("did not filter operation '%v'<>'%v'", test.expApplies, applies)
			}
			err := test.watch.fire(test.event)
			assertErr(t, test.expErr, err)
		})
	}
}

func TestWatchFS(t *testing.T) {
	mgr := newTestInstaller(t)
	operations := []fsnotify.Op{fsnotify.Create, fsnotify.Rename, fsnotify.Write}
	newWatch := func(path string, dst chan<- fsnotify.Event) watch {
		return watch{
			operations: operations,
			path:       path,
			eventFN: func(event fsnotify.Event) error {
				dst <- event
				return nil
			},
		}
	}
	type test struct {
		name           string
		expErr         string
		expWatchErrs   []string
		expWatchEvents []fsnotify.Event
		// doIO performs file I/O at the specified root
		doIO func(string) error
		// newWatchSet returns the slice of watches for the test
		newWatchSet func(string, chan<- fsnotify.Event) []watch
	}
	tests := []test{
		{
			name:           "EmptyWatchSet",
			expErr:         "",
			expWatchErrs:   nil,
			expWatchEvents: nil,
			doIO:           func(_ string) error { return nil },
			newWatchSet:    func(_ string, _ chan<- fsnotify.Event) []watch { return nil },
		},
		{
			name: "WatchRotateServiceAccountTokenWrite",
			newWatchSet: func(root string, dst chan<- fsnotify.Event) []watch {
				return []watch{newWatch(path.Join(root, "auth-token"), dst)}
			},
			expErr:       "",
			expWatchErrs: nil,
			expWatchEvents: []fsnotify.Event{
				{Name: "auth-token", Op: fsnotify.Write},
			},
			doIO: func(root string) error {
				// overwrite the watched auth-token file
				name := path.Join(root, "auth-token")
				data := []byte("LZtDhL^M48fZ#6CR7uyhgXeaM6SYoGFsv2NgQQK%M%o=")
				return os.WriteFile(name, data, writeFilePerm)
			},
		},
		{
			name: "WatchRotateServiceAccountTokenRename",
			newWatchSet: func(root string, dst chan<- fsnotify.Event) []watch {
				return []watch{newWatch(path.Join(root, "auth-token"), dst)}
			},
			expErr:       "",
			expWatchErrs: nil,
			// expWatchEvents expects only create as the watch is applied to
			// the inode vs the file name
			//
			// the renamed file's inode is removed and event doesn't fire.
			expWatchEvents: []fsnotify.Event{
				{Name: "auth-token", Op: fsnotify.Create},
			},
			doIO: func(root string) error {
				// rename one file on top of the watched auth-token file
				return os.Rename(path.Join(root, "auth-token-new"),
					path.Join(root, "auth-token"))
			},
		},
		{
			name: "WatchRotateServiceAccountTokenCreate",
			newWatchSet: func(root string, dst chan<- fsnotify.Event) []watch {
				return []watch{newWatch(root, dst)}
			},
			expErr:       "",
			expWatchErrs: nil,
			expWatchEvents: []fsnotify.Event{
				{Name: "created-auth-token", Op: fsnotify.Create},
			},
			doIO: func(root string) error {
				// create a new file relying on the directory being watched
				data := []byte("LZtDhL^M48fZ#6CR7uyhgXeaM6SYoGFsv2NgQQK%M%o=")
				return os.WriteFile(path.Join(root, "created-auth-token"), data,
					writeFilePerm)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			mustCopyFiles(t, root, "testdata")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			errs := make(chan error, len(test.expWatchErrs))
			events := make(chan fsnotify.Event, len(test.expWatchEvents))
			// watch for the test
			err := mgr.watchFS(ctx, errs, test.newWatchSet(root, events))
			if assertErr(t, test.expErr, err) {
				return
			}
			// perform test i/o on the tmp watch root
			err = test.doIO(root)
			if err != nil {
				t.Fatalf("cannot perform test i/o err=%v", err)
			}
			// collect events and errors from channels
			timeoutPeriod := time.Millisecond * 250
			timeout := time.NewTimer(timeoutPeriod)
			var actWatchEvents []fsnotify.Event
			for i := 0; i < len(test.expWatchEvents); i++ {
				select {
				case event := <-events:
					actWatchEvents = append(actWatchEvents, event)
				case <-timeout.C:
					t.Fatalf("timeout waiting %v for fs events", timeoutPeriod)
				}
			}
			var actWatchErrs []error
			for i := 0; i < len(test.expWatchErrs); i++ {
				select {
				case err := <-errs:
					actWatchErrs = append(actWatchErrs, err)
				case <-timeout.C:
					t.Fatalf("timeout waiting %v for fs errors", timeoutPeriod)
				}
			}
			// assert no watch errors
			if len(actWatchErrs) > 0 {
				t.Fatalf("exp-errors is not empty %+v", actWatchErrs)
			}
			// assert expected watch events
			if len(test.expWatchEvents) != len(actWatchEvents) {
				t.Fatalf("exp-events != act-events %d<>%d", len(test.expWatchEvents), len(actWatchEvents))
			}
			for i := 0; i < len(test.expWatchEvents); i++ {
				test.expWatchEvents[i].Name = path.Join(root, test.expWatchEvents[i].Name)
				if !reflect.DeepEqual(test.expWatchEvents[i], actWatchEvents[i]) {
					t.Fatalf("exp-events[%d]<>act-events[%d]\n%+v\n+%+v", i, i,
						test.expWatchEvents[i], actWatchEvents[i])
				}
			}
		})
	}
}
