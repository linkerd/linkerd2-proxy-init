# vim: set ft=make :
# See https://just.systems/man/en


########
# RUST #
########

# By default we compile in development mode mode because it's faster.
rs-build-type := if env_var_or_default("RELEASE", "") == "" { "debug" } else { "release" }

# Overriddes the default Rust toolchain version.
rs-toolchain := ""

rs-features := 'all'

_cargo := "cargo" + if rs-toolchain != "" { " +" + rs-toolchain } else { "" }

# Fetch Rust dependencies.
rs-fetch:
    {{ _cargo }} fetch --locked

# Format Rust code.
rs-fmt:
    {{ _cargo }} fmt --all

# Check that the Rust code is formatted correctly.
rs-check-fmt:
    {{ _cargo }} fmt --all -- --check

# Lint Rust code.
rs-clippy:
    {{ _cargo }} clippy --frozen --workspace --all-targets --no-deps {{ _features }} {{ _fmt }}

# Audit Rust dependencies.
rs-audit-deps:
    {{ _cargo }} deny {{ _features }} check

# Generate Rust documentation.
rs-doc *flags:
    {{ _cargo }} doc --frozen \
        {{ if rs-build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

# Compile Rust unit tests
rs-test-build:
    {{ _cargo }} test --no-run --frozen \
        --workspace \
        {{ _features }} \
        {{ _fmt }}

# Run Rust unit tests
rs-test *flags:
    {{ _cargo }} {{ _cargo-test }} --frozen \
        {{ if rs-build-type == "release" { "--release" } else { "" } }} \
        {{ _features }} \
        {{ flags }}

# Check each crate independently to ensure its Cargo.toml is sufficient.
rs-check-dirs:
    #!/usr/bin/env bash
    set -euo pipefail
    while IFS= read -r toml ; do
        {{ just_executable() }} \
            rs-build-type='{{ rs-build-type }}' \
            rs-features='{{ rs-features }}' \
            rs-toolchain='{{ rs-toolchain }}' \
            _rs-check-dir "${toml%/*}"
        {{ just_executable() }} \
            rs-build-type='{{ rs-build-type }}' \
            rs-features='{{ rs-features }}' \
            rs-toolchain='{{ rs-toolchain }}' \
            _rs-check-dir "${toml%/*}" --tests
    done < <(find . -mindepth 2 -name Cargo.toml | sort -r)

_rs-check-dir dir *flags:
    cd {{ dir }} \
        && {{ _cargo }} check --frozen \
                {{ if rs-build-type == "release" { "--release" } else { "" } }} \
                {{ _features }} \
                {{ flags }} \
                {{ _fmt }}

# If we're running in github actions and cargo-action-fmt is installed, then add
# a command suffix that formats errors.
_fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
    ```
    if command -v cargo-action-fmt >/dev/null 2>&1; then
        echo "--message-format=json | cargo-action-fmt"
    fi
    ```
}

# Configures which features to enable when invoking cargo commands.
_features := if rs-features == "all" {
        "--all-features"
    } else if rs-features != "" {
        "--no-default-features --features=" + rs-features
    } else { "" }

# Use nextest if it's available (i.e. when running locally).
_cargo-test := ```
    if command -v cargo-nextest >/dev/null 2>&1; then echo " nextest run"
    else echo " test" ; fi
    ```

##############
# PROXY-INIT #
##############

# Build the project
go-build:
    go build -o out/linkerd2-proxy-init ./proxy-init

# Runs Go's code formatting tool and succeeds if no output is printed
go-fmt:
    gofmt -d .
    test -z "$(gofmt -d .)"

# Run unit tests
go-test-unit:
    go test -v ./...

# Run integration tests
go-test-integration cluster='init-test': docker-proxy-init docker-tester
    k3d image import -c {{ cluster }} {{ docker_tester_tag }} {{ docker_tag }}
    cd integration_test && ./run_tests.sh

# Run all tests in a k3d cluster
go-test: go-test-unit go-test-integration

##########
# DOCKER #
##########

# If DOCKER_REGISTRY is not already set, use a bogus registry with a unique
# domain name so that it's virtually impossible to accidentally use an older
# cached image.
_test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`
export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test-" + _test-id + ".local/linkerd")


docker_repo := '${DOCKER_REGISTRY}' + "/proxy-init"
docker_tag := docker_repo + ":" + "latest"
amd64_arch := "linux/amd64"
# Build docker image for proxy-init (Development)
docker-proxy-init arch=amd64_arch:
    docker buildx build . \
    	--tag={{ docker_tag }} \
    	--platform={{ arch }} \
    	--load \

# Dev tag randomly generated based on git rev & hostname
docker_validator_tag := "dev-" + `git rev-parse --short=8 HEAD`
docker_validator_repo := '${DOCKER_REGISTRY}' + "/cni-validator"
# Build docker image for cni-validator on amd64 (Development)
docker-cni-validator:
	docker buildx build . \
		--tag={{ docker_validator_repo }}:{{ docker_validator_tag }} \
		--file="./cni-validator/amd64.dockerfile" \
		--build-arg='build_type={{ rs-build-type }}' \
		--load
	@echo {{ docker_validator_repo }}:{{ docker_validator_tag }}

docker_tester_tag := '${DOCKER_REGISTRY}' + "/iptables-tester:v1"
dockerfile_tester_path := "./integration_test/iptables/Dockerfile-tester"
# Build docker image for iptables-tester (Development)
docker-tester arch=amd64_arch:
    docker buildx build ./integration_test \
    	--file={{ dockerfile_tester_path }} \
    	--tag={{ docker_tester_tag }} \
    	--platform={{ arch }} \
    	--load

docker_cache_path := env_var_or_default("DOCKER_BUILDKIT_CACHE", "")
# Prune Docker BuildKit cache
docker-cache-prune:
    #!/usr/bin/env bash
    set -euxo pipefail
   
    # Deletes all files under the buildkit blob directory that are not referred
    # to any longer in the cache manifest file
    manifest_sha=$(jq -r .manifests[0].digest < "{{ docker_cache_path }}/index.json")
    manifest=${manifest_sha#"sha256:"}
    files=("$manifest")
    while IFS= read -r line; do files+=("$line"); done <<< "$(jq -r '.manifests[].digest | sub("^sha256:"; "")' < "{{ docker_cache_path }}/blobs/sha256/$manifest")"
    for file in "{{ docker_cache_path }}"/blobs/sha256/*; do
      name=$(basename "$file")
      if [[ ! "${files[@]}" =~ ${name} ]]; then
    	printf 'pruned from cache: %s\n' "$file"
    	rm -f "$file"
      fi
    done
