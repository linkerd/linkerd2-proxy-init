DOCKER_REGISTRY ?= gcr.io/linkerd-io
REPO = $(DOCKER_REGISTRY)/proxy-init

###############
# Go
###############
.PHONY: build
build:
	go build -o out/linkerd2-proxy-init main.go

.PHONY: run
run:
	go run main.go

.PHONY: test
test:
	go test -v ./...

.PHONY: fmt
fmt:
	gofmt -d .
	test -z "$(shell gofmt -d .)"

.PHONY: integration-test
integration-test: image
	cd integration_test && ./run_tests.sh

###############
# Docker
###############
.PHONY: image
image:
	docker build -t $(REPO):latest .

.PHONY: tester-image
tester-image:
	docker build -t buoyantio/iptables-tester:v1 -f ./integration_test/iptables/Dockerfile-tester ./integration_test

.PHONY: kind-load
kind-load: image tester-image
	kind load docker-image $(REPO):latest
	kind load docker-image buoyantio/iptables-tester:v1
