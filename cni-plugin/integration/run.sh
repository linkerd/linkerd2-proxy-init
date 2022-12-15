#!/usr/bin/env bash

set -euo pipefail

cd "${BASH_SOURCE[0]%/*}"

# Run kubectl with the correct context.
function k() {
  if [ -n "${TEST_CTX:-}" ]; then
    kubectl --context="$TEST_CTX" "$@"
  else
    kubectl "$@"
  fi
}

function cleanup() {
    echo '# Cleaning up...'
    k delete -f manifests/cni-plugin-lab.yaml
    k delete ns cni-plugin-test
}

trap cleanup EXIT

# Get the IP of a test pod.
function kip() {
    local name=$1
    k wait pod "$name" --namespace=cni-plugin-test \
        --for=condition=ready --timeout=1m \
        >/dev/null

    k get pod "$name" --namespace=cni-plugin-test \
        --template='{{.status.podIP}}'
}

if k get ns/cni-plugin-test >/dev/null 2>&1 ; then
  echo 'ns/cni-plugin-test already exists' >&2
  exit 1
fi

echo '# Creating the test lab...'
k create ns cni-plugin-test
k create -f manifests/cni-plugin-lab.yaml

# TODO(stevej): image-pull-policy should be changed to Never and
# the image should be the locally generated image labelled test
echo '# Running tester...'
k run cni-plugin-tester \
        --attach \
        --command \
        --image=test.l5d.io/linkerd/cni-plugin-tester:test \
        --image-pull-policy=Never \
        --namespace=cni-plugin-test \
        --restart=Never \
        --rm \
        -- \
        go test -v -integration-tests

