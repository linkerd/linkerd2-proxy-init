# vim: set ft=make :
# See https://just.systems/man/en
#
# Config
#

# If DOCKER_REGISTRY is not already set, use a bogus registry with a unique
# domain name so that it's virtually impossible to accidentally use an older
# cached image.
_test-id := `tr -dc 'a-z0-9' </dev/urandom | fold -w 5 | head -n 1`
export DOCKER_REGISTRY := env_var_or_default("DOCKER_REGISTRY", "test-" + _test-id + ".local/linkerd")

docker_repo := '${DOCKER_REGISTRY}' + "/proxy-init"
docker_tag := docker_repo + ":" + "latest"
docker_tester_tag := '${DOCKER_REGISTRY}' + "/iptables-tester:v1"
dockerfile_tester_path := "./integration_test/iptables/Dockerfile-tester"
amd64_arch := "linux/amd64"
docker_cache_path := env_var_or_default("DOCKER_BUILDKIT_CACHE", "")

#
# Recipes
#

# Run formatting, tests, and build the iptables container
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
    k3d image import -c {{ cluster }} {{ docker_tester_tag }} {{ docker_tag }}
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
    	--tag={{ docker_tag }} \
    	--platform={{ arch }} \
    	--load \

# Build docker image for iptables-tester (Development)
docker-tester arch=amd64_arch:
    docker buildx build ./integration_test \
    	--file={{ dockerfile_tester_path }} \
    	--tag={{ docker_tester_tag }} \
    	--platform={{ arch }} \
    	--load

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
