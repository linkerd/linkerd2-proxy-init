package cni

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

// Run performs a single static installation of all binaries from the directory
// container's install directory to directory representing the underlying host's
// mount point for plugin binaries.
//
// It re-writes the kubeconfig file inserting the service account token.
//
// It re-writes cni configuration injecting linkerd.
//
// It use inotify (via fsnotify) to register watches against the service
// account token file, as well as the cni configuration root. If events for
// either watch fire the corresponding configuration is rewritten.
//
// If an error occurs it is returned.
func (i *installer) Run(ctx context.Context) error {
	installed, err := i.installRegularFiles(hostCNIBin(),
		containerCNIBinDir.get())
	if err != nil {
		return err
	}
	log.WithField("installed-files", installed).Debug("installed binary files")
	watchOperations := []fsnotify.Op{
		fsnotify.Create,
		fsnotify.Rename,
		fsnotify.Write,
	}
	watches := []watch{
		{
			// this watch monitors the parent directory of the service account
			// token file (/var/run/secrets/kubernetes.io/serviceaccount)
			//
			// it further filters on '..data' events specifically
			//
			// kubernetes will make atomic changes to the set of files (token,
			// ca.crt and namespace) by:
			//   * writing the files into a timestamped directory (..2026_06_26_17_28_56.1664091420)
			//	 * symlinking a '..data_tmp' to the timestamped directory
			//	 * renaming '..data_tmp' to '..data'
			//	 * creating root directory symlinks to '..data/${file}'
			// see https://github.com/kubernetes/kubernetes/blob/release-1.32/pkg/volume/util/atomic_writer.go#L86-L138
			//
			// by watching the parent for changes to '..data' no new watches
			// need to be added when the underlying files are changed
			// see https://github.com/fsnotify/fsnotify/blob/v1.10.1/fsnotify.go#L215
			eventFN: func(event fsnotify.Event) error {
				if strings.HasSuffix(event.Name, "..data") {
					log.WithField("event", fmtEvent(event)).
						Debug("fsnotify event fired -> reconfigure k8s")
					return i.reconfigureK8s(kubeConfigFilename(),
						i.serviceAccountTokenFilename)
				}
				log.WithField("event", fmtEvent(event)).
					Debug("fsnotify event fired -> ignore")
				return nil
			},
			operations: watchOperations,
			path:       path.Dir(i.serviceAccountTokenFilename),
		},
		{
			eventFN: func(event fsnotify.Event) error {
				if isCNIFile(event.Name) {
					log.WithField("event", fmtEvent(event)).
						Debug("fsnotify event fired -> reconfigure cni")
					return i.reconfigureCNI(event.Name)
				}
				log.WithField("event", fmtEvent(event)).
					Debug("fsnotify event fired -> ignore non-cni-file")
				return nil
			},
			operations: watchOperations,
			path:       hostCNIConfig(),
		},
	}
	errs := make(chan error, 1)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	err = i.watchFS(ctx, errs, watches)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"watches": watches,
	}).Debug("watching filesystem changes")
	// send an event to reconfigure the kube config file
	i.watcherEvents <- fsnotify.Event{
		Op:   fsnotify.Create,
		Name: path.Join(path.Dir(i.serviceAccountTokenFilename), "..data")}
	// send events to reconfigure cni config files
	entries, err := os.ReadDir(hostCNIConfig())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		i.watcherEvents <- fsnotify.Event{
			Op:   fsnotify.Write,
			Name: path.Join(hostCNIConfig(), entry.Name())}
	}
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		return nil
	}
}

// fmtEvent returns a log friendly string of the event
func fmtEvent(e fsnotify.Event) string {
	return fmt.Sprintf("name=%s op=%-13s", e.Name, e.Op.String())
}

// isCNIFile pulls the file extension from filename and returns true if it
// matches file target types that can be re-written.
func isCNIFile(filename string) bool {
	ext := path.Ext(filename)
	return ext == ".conflist" || ext == ".conf"
}
