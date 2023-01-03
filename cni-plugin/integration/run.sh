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

function create_test_lab() {
    echo '# Creating the test lab...'
    k create ns cni-plugin-test
    k create serviceaccount linkerd-cni
    k create -f manifests/linkerd-cni.yaml
}

function cleanup() {
    echo '# Cleaning up...'
    k delete -f manifests/cni-plugin-lab.yaml
    k delete -f manifests/linkerd-cni.yaml
    k delete serviceaccount linkerd-cni
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

create_test_lab
# TODO(stevej): this would be nicer if it checked on status a few times
# before exiting so we can reduce the total amount of time waiting
sleep 10
if ! k rollout status daemonset/linkerd-cni -n linkerd-cni; then
  echo "linkerd-cni didn't rollout properly, check logs";
  exit $?
fi

echo "# linkerd-cni is running, starting first cni-plugin test..."
k create -f manifests/cni-plugin-lab.yaml

# TODO(stevej): instead of running `go test`, have cni-plugin-tester:test
# use `go test`` as an entrypoint and run that instead of nginx
echo '# Running tester...'
#k run cni-plugin-tester \
#        --command \
#        --attach \
#        --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
#        --image-pull-policy=Never \
#        --namespace=cni-plugin-test \
#        --restart=Never \
#        -- \
#        go test -v ./cni-plugin/integration/... -integration-tests
