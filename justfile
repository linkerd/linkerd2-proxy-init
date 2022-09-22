# vim: set ft=make :
# See https://just.systems/man/en
#
# Config
#

_image := "test.l5d.io/linkerd/proxy-init:test"
_test-image := "test.l5d.io/linkerd/iptables-tester:test"
docker-arch := "linux/amd64"

##
## Recipes
##

default: lint test

lint: sh-lint md-lint rs-clippy proxy-init-lint action-lint action-dev-check

build: proxy-init-build validator-build

test: rs-test proxy-init-test-unit proxy-init-test-integration

# Check whether the Go code is formatted.
go-fmt-check:
    out=$(gofmt -d .) ; [ -z "$out" ] || (echo "$out" ; exit 1)

##
## rust
##

# By default we compile in development mode because it's faster
rs-build-type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

# Overrides the default Rust toolchain version
rs-toolchain := ""

_cargo := "cargo" + if rs-toolchain != "" { " +" + rs-toolchain } else { "" }

# Fetch Rust dependencies
rs-fetch:
	{{ _cargo }} fetch --locked

# Format Rust code
rs-fmt-check:
	{{ _cargo }} fmt --all -- --check

# Lint Rust code
rs-clippy:
	{{ _cargo }} clippy --frozen --workspace --all-targets --no-deps {{ _cargo-fmt }}

# Audit Rust dependencies
rs-audit-deps:
	{{ _cargo }} deny check

# Build Rust unit and integration tests
rs-test-build:
	{{ _cargo-test }} --no-run --frozen --workspace {{ _cargo-fmt }}

# Run unit tests in whole Rust workspace
rs-test *flags:
	{{ _cargo-test }} --frozen --workspace \
		{{ if rs-build-type == "release" { "--release" } else { "" } }} \
		{{ flags }}

# Check a specific Rust crate
rs-check-dir dir *flags:
	cd {{ dir }} \
		&& {{ _cargo }} check --frozen \
		{{ if rs-build-type == "release" { "--release" } else { "" } }} \
		{{ flags }} \
		{{ _cargo-fmt }}

# If recipe is run in github actions (and cargo-action-fmt is installed), then add a
# command suffix that formats errors
_cargo-fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
    ```
    if command -v cargo-action-fmt >/dev/null 2>&1 ; then
        echo "--message-format=json | cargo-action-fmt"
    fi
    ```
}

# When available, use cargo-nextest to run Rust tests; if the binary is not available,
# use default test runner
_cargo-test := _cargo + ```
	if command -v cargo-nextest >/dev/null 2>&1 ; then
		echo " nextest run"
	else
		echo " test"
	fi
```

##
## validator
##

# Build validator code
validator-build *flags:
	{{ _cargo }} build --workspace -p linkerd-network-validator \
		{{ if rs-build-type == "release" { "--release" } else { "" } }} \
		{{ flags }}

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

# Run integration tests without preparing dependencies
proxy-init-test-integration-run:
    TEST_CTX='k3d-{{ k3d-name }}' ./proxy-init/integration/run.sh

# Build and load images
proxy-init-test-integration-deps: proxy-init-image proxy-init-test-image _k3d-init
    {{ _k3d-load }} {{ _test-image }} {{ _image }}

# Build docker image for proxy-init (Development)
proxy-init-image:
    docker buildx build . \
    	--tag={{ _image }} \
    	--platform={{ docker-arch }} \
    	--load \

# Build docker image for iptables-tester (Development)
proxy-init-test-image:
    docker buildx build . \
        --file=proxy-init/integration/iptables/Dockerfile-tester \
    	--tag={{ _test-image }} \
    	--platform={{ docker-arch }} \
    	--load

##
## Test cluster
##

# The name of the k3d cluster to use.
k3d-name := "l5d-test"

# The kubernetes version to use for the test cluster. e.g. 'v1.24', 'latest', etc
k3d-k8s := "latest"

k3d-agents := "0"
k3d-servers := "1"

_context := "--context=k3d-" + k3d-name
_kubectl := "kubectl " + _context

_k3d-load := "k3d image import --mode=direct --cluster=" + k3d-name

# Run kubectl with the test cluster context.
k *flags:
    {{ _kubectl }} {{ flags }}

# Creates a k3d cluster that can be used for testing.
k3d-create: && _k3d-ready
    k3d cluster create {{ k3d-name }} \
        --image='+{{ k3d-k8s }}' \
        --agents='{{ k3d-agents }}' \
        --servers='{{ k3d-servers }}' \
        --no-lb \
        --k3s-arg '--disable=local-storage,traefik,servicelb,metrics-server@server:*' \
        --kubeconfig-update-default \
        --kubeconfig-switch-context=false

# Deletes the test cluster.
k3d-delete:
    k3d cluster delete {{ k3d-name }}

# Print information the test cluster's detailed status.
k3d-info:
    k3d cluster list {{ k3d-name }} -o json | jq .

# Ensures the test cluster has been initialized.
_k3d-init: && _k3d-ready
    #!/usr/bin/env bash
    set -euo pipefail
    if ! k3d cluster list {{ k3d-name }} >/dev/null 2>/dev/null; then
        {{ just_executable() }} \
            k3d-name={{ k3d-name }} \
            k3d-k8s={{ k3d-k8s }} \
            k3d-create
    fi
    k3d kubeconfig merge l5d-test \
        --kubeconfig-merge-default \
        --kubeconfig-switch-context=false \
        >/dev/null

_k3d-ready: _k3d-api-ready _k3d-dns-ready

# Wait for the cluster's API server to be accessible
_k3d-api-ready:
    #!/usr/bin/env bash
    set -euo pipefail
    for i in {1..6} ; do
        if {{ _kubectl }} cluster-info >/dev/null ; then exit 0 ; fi
        sleep 10
    done
    exit 1

# Wait for the cluster's DNS pods to be ready.
_k3d-dns-ready:
    while [ $({{ _kubectl }} get po -n kube-system -l k8s-app=kube-dns -o json |jq '.items | length') = "0" ]; do sleep 1 ; done
    {{ _kubectl }} wait pod --for=condition=ready \
        --namespace=kube-system --selector=k8s-app=kube-dns \
        --timeout=1m

##
## CI utilities
##

# Format actionlint output for Github Actions if running in CI.
_actionlint-fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
  '{{range $err := .}}::error file={{$err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{$err.Message}}%0A```%0A{{replace $err.Snippet "\\n" "%0A"}}%0A```\n{{end}}'
}

# Lints all GitHub Actions workflows
action-lint:
    actionlint \
        {{ if _actionlint-fmt != '' { "-format '" + _actionlint-fmt + "'" } else { "" } }} \
        .github/workflows/*

action-dev-check:
    action-dev-check

md-lint:
    markdownlint-cli2 '**/*.md' '!target'

# Lints all shell scripts in the repo.
sh-lint:
    #!/usr/bin/env bash
    set -euo pipefail
    files=$(while IFS= read -r f ; do
        if [ "$(file -b --mime-type "$f")" = text/x-shellscript ]; then echo "$f"; fi
    done < <(find . -type f ! \( -path ./.git/\* -or -path \*/target/\* \)) | xargs)
    echo "shellcheck $files" >&2
    shellcheck $files

# Prune Docker BuildKit cache
docker-cache-prune dir:
    #!/usr/bin/env bash
    set -euxo pipefail
    # Delete all files under the buildkit blob directory that are not referred
    # to any longer in the cache manifest file
    manifest_sha=$(jq -r .manifests[0].digest < '{{ dir }}/index.json')
    manifest=${manifest_sha#"sha256:"}
    files=("$manifest")
    while IFS= read -r f; do
        files+=("$f")
    done < <(jq -r '.manifests[].digest | sub("^sha256:"; "")' <'{{ dir }}/blobs/sha256/$manifest')
    for file in '{{ dir }}'/blobs/sha256/*; do
      name=$(basename "$file")
      if [[ ! "${files[@]}" =~ ${name} ]]; then
    	printf 'pruned from cache: %s\n' "$file"
    	rm -f "$file"
      fi
    done
