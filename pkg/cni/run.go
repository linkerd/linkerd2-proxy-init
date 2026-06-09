package cni

import (
	"context"
	"fmt"
	"os"
	"path"

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
	err = i.reconfigureK8s(kubeConfigFilename(), i.serviceAccountTokenFilename)
	if err != nil {
		return err
	}
	log.WithField("kube-config-filename", kubeConfigFilename()).
		Debug("reconfigured k8s")
	entries, err := os.ReadDir(hostCNIConfig())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if isCNIFile(entry.Name()) {
			configFilename := path.Join(hostCNIConfig(), entry.Name())
			err = i.reconfigureCNI(configFilename)
			if err != nil {
				return err
			}
			log.WithField("config-filename", configFilename).
				Debug("reconfigured cni")
		}
	}
	if len(entries) < 1 {
		log.Warn("reconfigured 0 cni config files")
	}
	watchOperations := []fsnotify.Op{
		fsnotify.Create,
		fsnotify.Rename,
		fsnotify.Write,
	}
	watches := []watch{
		{
			eventFN: func(event fsnotify.Event) error {
				log.WithField("event", fmtEvent(event)).
					Debug("fsnotify event fired -> reconfigure k8s")
				return i.reconfigureK8s(kubeConfigFilename(), event.Name)
			},
			operations: watchOperations,
			path:       i.serviceAccountTokenFilename,
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
