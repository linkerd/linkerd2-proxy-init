#
# Config
#

proxy-init-image := "test.l5d.io/linkerd/proxy-init:test"
_test-image := "test.l5d.io/linkerd/iptables-tester:test"

##
## Recipes
##

default: lint test

lint: sh-lint md-lint rs-clippy proxy-init-lint action-lint action-dev-check

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
## proxy-init
##

proxy-init-build:
    go build -o target/linkerd2-proxy-init ./proxy-init

proxy-init-lint:
    golangci-lint run ./proxy-init/...

# Run proxy-init unit tests
proxy-init-test-unit:
    go test -v ./proxy-init/...

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
## Test cluster
##

export K3S_DISABLE := "local-storage,traefik,servicelb,metrics-server@server:*"
export K3D_CREATE_FLAGS := '--no-lb'

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
