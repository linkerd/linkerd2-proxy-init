DOCKER_REGISTRY ?= cr.l5d.io/linkerd
CLUSTER_NAME ?= k3s-default
REPO = $(DOCKER_REGISTRY)/proxy-init
TESTER_REPO = $(DOCKER_REGISTRY)/iptables-tester
VERSION ?= $(shell git describe --exact-match --tags 2> /dev/null || git rev-parse --short HEAD)
SUPPORTED_ARCHS = linux/amd64,linux/arm64,linux/arm/v7
PUSH_IMAGE ?= false
ARCH ?= linux/amd64
TAG ?= $(shell bin/dev_tag.sh)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
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
integration-test: image-load ## Run integration tests in k3d
	cd integration_test && ./run_tests.sh

###############
# Docker
###############
.PHONY: proxy-init-image
proxy-init-image: ## Build docker image for proxy-init
	DOCKER_BUILDKIT=1 docker build -t "$(REPO):$(TAG)" .

.PHONY: init-tester-image
init-tester-image: ## Build docker image for the tester component
	DOCKER_BUILDKIT=1 docker build \
		-t "$(TESTER_REPO):v1" \
		-f ./integration_test/iptables/Dockerfile-tester \
		./integration_test

.PHONY: image-load
image-load: proxy-init-image init-tester-image ## Load proxy-init and tester image into k3d cluster
	bin/k3d image import -c "$(CLUSTER_NAME)" "$(REPO):$(TAG)" "$(TESTER_REPO):v1"
		
.PHONY: images
images: ## Build multi arch docker images for the project
	docker buildx build \
		--platform $(SUPPORTED_ARCHS) \
		--output "type=image,push=$(PUSH_IMAGE)" \
		--tag $(REPO):$(VERSION) \
		.

.PHONY: push
push: ## Push multi arch docker images to the registry
	PUSH_IMAGE=true make images

.PHONY: inspect-manifest
inspect-manifest: ## Check the resulting images supported architecture
	docker run --rm mplatform/mquery $(REPO):$(VERSION)

.PHONY: builder
builder: ## Create the Buildx builder instance
	docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
	docker buildx create --name=multiarch-builder --driver=docker-container --platform="$(SUPPORTED_ARCHS)" --use
	docker buildx inspect multiarch-builder --bootstrap
