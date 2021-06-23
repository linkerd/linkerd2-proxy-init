package main

import (
	"log"
	"os"

	"github.com/linkerd/linkerd2-proxy-init/cmd"
)

func main() {
	log.SetOutput(os.Stdout)

	if err := cmd.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
