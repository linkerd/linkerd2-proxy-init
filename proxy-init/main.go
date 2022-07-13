package main

import (
	"os"

	"github.com/linkerd/linkerd2-proxy-init/proxy-init/cmd"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetOutput(os.Stdout)

	if err := cmd.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
