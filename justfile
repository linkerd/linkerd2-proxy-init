registry := "cr.l5d.io/linkerd"
docker_repo := registry + "/proxy-init"
dev_tag := "dev-" + `git rev-parse --short=8 HEAD` + "-" + env_var("USER")
docker_tag := docker_repo + ":" + dev_tag
docker_tester_tag := registry + "/iptables-tester:v1" 
dockerfile_tester_path := "./integration_test/iptables/Dockerfile-tester"
amd64_arch := "linux/amd64"

#
# Recipes
#

#
default: fmt test build

# Build the project
build:
  go build -o out/linkerd2-proxy-init main.go

# Runs Go's code formatting tool and succeeds if no output is printed
fmt:
  gofmt -d .
  test -z "$(gofmt -d .)"

# Run unit tests
test-unit:
  go test -v ./...

# Run integration tests
test-integration cluster='init-test':
  k3d image import -c init-test {{docker_tester_tag}} {{docker_tag}}
  cd integration_test && ./run_tests.sh

# Run all tests in a k3d cluster
test:
	#!/usr/bin/env bash
	set -eu
	just test-unit
	just docker-proxy-init
	just docker-tester
	just test-integration

# Build docker image for proxy-init (Development)
docker-proxy-init arch=amd64_arch:
	docker buildx build . \
		--tag={{docker_tag}} \
		--platform={{arch}} \
		--load \

# Build docker image for iptables-tester (Development)
docker-tester arch=amd64_arch:
	docker buildx build ./integration_test \
		--file={{dockerfile_tester_path}} \
		--tag={{docker_tester_tag}} \
		--platform={{arch}} \
		--load
	
# vim: set ft=make :
