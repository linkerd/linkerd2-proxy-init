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
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	err := cni.NewInstaller().Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithFields(logrus.Fields{"err": err}).Fatal("cannot run cni installer")
	}
}
