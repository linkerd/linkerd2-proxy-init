package main

import (
	"os"

	log "github.com/sirupsen/logrus"

	"github.com/linkerd/linkerd2-proxy-init/proxy-init/cmd"
)

func main() {
	log.SetOutput(os.Stdout)

	if err := cmd.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
