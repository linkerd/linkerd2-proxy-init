// Command cni-install supervises the cni plugin state using the manager.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/linkerd/linkerd2-proxy-init/pkg/cni"
	"github.com/sirupsen/logrus"
)

const (
	// defaultLevel is used if the provided level cannot be parsed.
	defaultLevel = logrus.DebugLevel
)

// flags provided by parsing command line args.
var flags struct {
	// logLevel override
	logLevel string
}

func main() {
	flag.StringVar(&flags.logLevel, "log-level", defaultLevel.String(),
		fmt.Sprintf("installer log level: %q", logrus.AllLevels))
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan os.Signal, 1)
	go func() {
		<-ch
		cancel()
	}()
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

	level, err := logrus.ParseLevel(flags.logLevel)
	if err != nil {
		_, _ = fmt.Fprintf(flag.CommandLine.Output(),
			"Cannot parse log-level '%s'\n", flags.logLevel)
		os.Exit(1)
	}
	logrus.SetLevel(level)
	logrus.Info("running installer")
	installer := cni.NewInstaller()
	defer func() {
		if err := installer.Remove(); err != nil {
			logrus.WithError(err).Fatal("cannot uninstall cni")
		}
	}()
	err = installer.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		logrus.WithError(err).Fatal("cannot run cni install")
	}
}
