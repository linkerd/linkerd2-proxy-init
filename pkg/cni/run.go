package cni

import (
	"context"
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
	installed, err := i.installRegularFiles(hostCNIBin(), containerCNIBinDir.get())
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"installed-files": installed,
	}).Debug("cni installed binary files")
	err = i.reconfigureK8s(kubeConfigFilename(), i.serviceAccountTokenFilename)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"kube-config-filename": kubeConfigFilename(),
	}).Debug("cni reconfigured kube-config")
	entries, err := os.ReadDir(hostCNIConfig())
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".conflist") {
			configFilename := path.Join(hostCNIConfig(), entry.Name())
			err = i.reconfigureCNI(configFilename)
			if err != nil {
				return err
			}
			log.WithFields(log.Fields{
				"config-filename": configFilename,
			}).Debug("cni reconfigured cni file")
		}
	}
	if len(entries) < 1 {
		log.Warn("cni reconfigured 0 cni config files")
	}
	watchOperations := []fsnotify.Op{
		fsnotify.Create,
		fsnotify.Rename,
		fsnotify.Write,
	}
	saDir := path.Dir(i.serviceAccountTokenFilename)
	watches := []watch{
		{
			eventFN: func(event fsnotify.Event) error {
				return i.reconfigureK8s(
					kubeConfigFilename(), path.Join(saDir, event.Name))
			},
			operations: watchOperations,
			path:       i.serviceAccountTokenFilename,
		},
		{
			eventFN: func(event fsnotify.Event) error {
				return i.reconfigureCNI(path.Join(hostCNIConfig(), event.Name))
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
