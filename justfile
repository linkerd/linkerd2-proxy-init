#
# Config
#

proxy-init-image := "test.l5d.io/linkerd/proxy-init:test"
_test-image := "test.l5d.io/linkerd/iptables-tester:test"
cni-plugin-image := "test.l5d.io/linkerd/cni-plugin:test"
_cni-plugin-test-image := "test.l5d.io/linkerd/cni-plugin-tester:test"

##
## Recipes
##

default: lint test

lint: sh-lint md-lint rs-clippy action-lint action-dev-check

go-lint *flags: (proxy-init-lint flags) (cni-plugin-lint flags)

test: rs-test proxy-init-test-unit proxy-init-test-integration

# Check whether the Go code is formatted.
go-fmt-check:
    out=$(gofmt -d .) ; [ -z "$out" ] || (echo "$out" ; exit 1)

##
## rust
##

# By default we compile in development mode because it's faster
rs-profile := "debug"

rs-target := "x86_64-unknown-linux-gnu"

# Overrides the default Rust toolchain version
rs-toolchain := ""

_cargo := 'just-cargo profile=' + rs-profile + ' target='  + rs-target + ' toolchain=' + rs-toolchain

# Fetch Rust dependencies
rs-fetch:
    {{ _cargo }} fetch --locked

# Format Rust code
rs-fmt-check:
    {{ _cargo }} fmt --all -- --check

# Lint Rust code
rs-clippy:
    {{ _cargo }} clippy --all-targets --no-deps

# Audit Rust dependencies
rs-audit-deps:
    cargo-deny --all-features check

# Build Rust unit and integration tests
rs-test-build:
    {{ _cargo }} test-build --workspace --no-run

# Run unit tests in whole Rust workspace
rs-test *flags:
    {{ _cargo }} test --workspace {{ flags }}

# Check a specific Rust crate
rs-check-dir dir *flags:
    cd {{ dir }} \
        && {{ _cargo }} check {{ flags }}

##
## validator
##

validator *args:
    {{ just_executable() }} --justfile=validator/.justfile {{ args }}

##
## cni-plugin
##

cni-plugin-lint *flags:
    golangci-lint run ./cni-plugin/... {{ flags }}

##
## proxy-init
##

proxy-init-build:
    go build -o target/linkerd2-proxy-init ./proxy-init

proxy-init-lint *flags:
    golangci-lint run ./proxy-init/... {{ flags }}

# Run proxy-init unit tests
proxy-init-test-unit:
    go test -v ./proxy-init/...
    go test -v ./internal/...

# Run proxy-init integration tests after preparing dependencies
proxy-init-test-integration: proxy-init-test-integration-deps proxy-init-test-integration-run

# Build and load images
proxy-init-test-integration-deps: build-proxy-init-image build-proxy-init-test-image _k3d-ready
    @just-k3d import {{ _test-image }} {{ proxy-init-image }}

# Run integration tests without preparing dependencies
proxy-init-test-integration-run:
    TEST_CTX="k3d-$(just-k3d --evaluate K3D_CLUSTER_NAME)" ./proxy-init/integration/run.sh

# Build docker image for proxy-init (Development)
build-proxy-init-image *args='--load':
    docker buildx build . --tag={{ proxy-init-image }} {{ args }}

# Build docker image for iptables-tester (Development)
build-proxy-init-test-image *args='--load':
    docker buildx build . \
        --file=proxy-init/integration/iptables/Dockerfile-tester \
        --tag={{ _test-image }} \
        {{ args }}

##
## cni-plugin
##

cni-plugin-build:
    go build -o target/linkerd2-cni-plugin ./cni-plugin

cni-plugin-test-unit:
    go test -v ./cni-plugin/...

# TODO(stevej): this does not run within the devcontainer
cni-plugin-installer-integration-run: build-cni-plugin-image
    HUB=test.l5d.io/linkerd TAG=test go test -cover -v -mod=readonly ./cni-plugin/test/... -integration-tests

# Build docker image for cni-plugin (Development)
build-cni-plugin-image *args='--load':
    docker buildx build . \
        --file=Dockerfile-cni-plugin \
        --tag={{ cni-plugin-image }} \
        {{ args }}

# Build docker image for cni-plugin-tester (Development)
build-cni-plugin-test-image *args='--load':
    docker buildx build . \
        --file=cni-plugin/integration/Dockerfile-tester \
        --tag={{ _cni-plugin-test-image }} \
        {{ args }}

# Build and load images for cni-plugin
cni-plugin-test-integration-deps: build-cni-plugin-image build-cni-plugin-test-image _k3d-ready
    @just-k3d import {{ _cni-plugin-test-image }} {{ cni-plugin-image }}

# Run cni-plugin integration tests after preparing dependencies
# For new scenarios, add them after cni-plugin-test-integration-deps
cni-plugin-test-integration: cni-plugin-test-integration-deps cni-plugin-test-integration-flannel

# Run flannel integration tests without preparing dependencies
cni-plugin-test-integration-flannel:
    SCENARIO=flannel TEST_CTX="k3d-$(just-k3d --evaluate K3D_CLUSTER_NAME)" ./cni-plugin/integration/run.sh



# TODO(stevej): add a k3d-create-debug
export K3S_DISABLE := "local-storage,traefik,servicelb,metrics-server@server:*"
export K3D_CREATE_FLAGS := '--no-lb --k3s-arg "--debug@server:*"'

# Creates a k3d cluster that can be used for testing.
k3d-create:
    @just-k3d create

# Deletes the test cluster.
k3d-delete:
    @just-k3d delete

# Print information the test cluster's detailed status.
k3d-info:
    @just-k3d info

_k3d-ready:
    @just-k3d ready

##
## CI utilities
##

# Lints all GitHub Actions workflows
action-lint:
    @just-dev lint-actions

action-dev-check:
    @just-dev check-action-images

md-lint:
    @just-md lint

# Lints all shell scripts in the repo.
sh-lint:
    @just-sh lint
