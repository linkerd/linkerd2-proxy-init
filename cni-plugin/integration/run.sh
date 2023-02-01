#!/usr/bin/env bash

set -euxo pipefail

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
    # TODO(stevej): how can we parameterize this manifest with `version` so we
    # can enable a testing matrix?
    k create -f manifests/linkerd-cni.yaml
}

function cleanup() {
    echo '# Cleaning up...'
    k delete -f manifests/linkerd-cni.yaml || echo "could not delete -f manifests/linkerd-cni.yaml"
    k delete serviceaccount linkerd-cni || echo "could not delete serviceaccount linkerd-cni"
    k delete ns cni-plugin-test || echo "could not delete namespace cni-plugin-test"
}

function install_calico() {
  echo '# Installing Calico...'
  local yaml="https://k3d.io/v5.3.0/usage/advanced/calico.yaml"
  k apply -f $yaml || echo "could not apply $yaml"
}

trap cleanup EXIT

if k get ns/cni-plugin-test >/dev/null 2>&1 ; then
  echo 'ns/cni-plugin-test already exists' >&2
  exit 1
fi

create_test_lab

# Wait for linkerd-cni daemonset to complete
if ! k rollout status --timeout=30s daemonset/linkerd-cni -n linkerd-cni; then
  echo "!! linkerd-cni didn't rollout properly, printing logs";
  k describe ds linkerd-cni || echo "daemonset linkerd-cni not found"
  k logs linkerd-cni -n linkerd-cni || echo "logs not found for linkerd-cni"
  exit $?
fi

# the integration tests to run. pass in as an environment variable.
# defaults to the tests in the flannel subdirectory
SCENARIO=${SCENARIO-flannel}
if [ "$SCENARIO" == "calico" ]; then
  install_calico
fi
# TODO(stevej): we don't want to rely on a linkerd build in this repo, we
# can package network-validator separately.
echo '# Run the network validator...'
k run linkerd-proxy \
    --attach \
    -i \
    --command \
    --image="cr.l5d.io/linkerd/proxy:edge-22.12.1" \
    --image-pull-policy=IfNotPresent \
    --namespace=cni-plugin-test \
    --restart=Never \
    --rm \
    -- \
      --log-level debug --connect-addr 1.1.1.1:20001 \
      --listen-addr 0.0.0.0:4140 --timeout 10s

echo 'PASS: Network Validator'

# This needs to use the name linkerd-proxy so that linkerd-cni will run.
echo '# Running tester...'
k run linkerd-proxy \
        --attach \
        --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
        --image-pull-policy=Never \
        --namespace=cni-plugin-test \
        --restart=Never \
        --overrides="{
               \"apiVersion\": \"v1\",
               \"spec\": {
                  \"containers\": [
                     {
                        \"name\": \"linkerd-proxy\",
                        \"image\": \"test.l5d.io/linkerd/cni-plugin-tester:test\",
                        \"command\": [\"go\", \"test\", \"-v\", \"./cni-plugin/integration/${SCENARIO}...\", \"-integration-tests\"],
                        \"volumeMounts\": [
                           {
                              \"mountPath\": \"/var/lib/rancher/k3s/agent/etc/cni/net.d\",
                              \"name\": \"cni-net-dir\"
                           }
                        ]
                     }
                  ],
                  \"volumes\": [
                     {
                        \"name\": \"cni-net-dir\",
                        \"hostPath\": {
                           \"path\": \"/var/lib/rancher/k3s/agent/etc/cni/net.d\"
                        }
                     }
                  ]
               },
               \"status\": {}
            }" \
        --rm
