DOCKER_REGISTRY ?= gcr.io/linkerd-io
REPO = $(DOCKER_REGISTRY)/proxy-init
TESTER_REPO = buoyantio/iptables-tester

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo 'Info: For integration test using KinD, run make kind-load integration-test'
	@echo 'Info: For other environments, run make integration-test after having uploaded the images'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

###############
# Go
###############
.PHONY: build
build: ## Build the project
	go build -o out/linkerd2-proxy-init main.go

.PHONY: run
run: ## Run the project
	go run main.go

.PHONY: test
test: ## Perform unit test
	go test -v ./...

.PHONY: fmt
fmt: ## Check the code formatting
	gofmt -d .
	test -z "$(shell gofmt -d .)"

.PHONY: integration-test
integration-test: image ## Perform integration test
	cd integration_test && ./run_tests.sh

###############
# Docker
###############
.PHONY: image
image: ## Build docker image for the project
	docker build -t $(REPO):latest .

.PHONY: tester-image
tester-image: ## Build docker image for the tester component
	docker build -t $(TESTER_REPO):v1 -f ./integration_test/iptables/Dockerfile-tester ./integration_test

.PHONY: kind-load
kind-load: image tester-image ## Load the required image to KinD cluster
	kind load docker-image $(REPO):latest
	kind load docker-image $(TESTER_REPO):v1
