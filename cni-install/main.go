// Command cni-install supervises the cni plugin state using the manager.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2-proxy-init/pkg/cni"
	"github.com/sirupsen/logrus"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan os.Signal, 1)
	go func() {
		<-ch
		cancel()
	}()
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	logrus.SetLevel(logrus.DebugLevel)
	logrus.Info("running installer")
	installer := cni.NewInstaller()
	defer func() {
		if err := installer.Remove(); err != nil {
			logrus.WithError(err).Fatal("cannot uninstall cni")
		}
	}()
	err := installer.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithError(err).Fatal("cannot run cni install")
	}
}
