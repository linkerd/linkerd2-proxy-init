#!/usr/bin/env bash

set -euo pipefail

cd "${BASH_SOURCE[0]%/*}"

# Integration tests to run. Scenario is passed in as an environment variable.
# Default is 'flannel'
SCENARIO=${CNI_TEST_SCENARIO:-flannel}
echo "SCENARIO: $SCENARIO"

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
    # Apply all files in scenario directory. For non-flannel CNIs, this will
    # include the CNI manifest itself.
    k apply -f "manifests/$SCENARIO/"
}

# TODO (matei): skip this based on env var? Useful when running locally to see
# err messages
function cleanup() {
    echo '# Cleaning up...'
    k delete -f "manifests/$SCENARIO/linkerd-cni.yaml" || echo "could not delete -f manifests/linkerd-cni.yaml"
    k delete serviceaccount linkerd-cni || echo "could not delete serviceaccount linkerd-cni"
    k delete ns cni-plugin-test || echo "could not delete namespace cni-plugin-test"

    # Collect other files that are not related to linkerd-cni. This may include
    # CNI config files or install manifests
    local files="$(ls "manifests/$SCENARIO/" | grep -v "linkerd")"
    if [ -z "$files" ]; then
      k delete -f "$files" || echo "could not delete test resources"
    fi
}

function wait_rollout() {
  local name="$1"
  local ns="$2"
  local timeout="$3"

  if ! k rollout status --timeout="$timeout" "$name" -n "$ns"; then
    echo "!! $name didn't rollout properly, printing logs";
    k describe "$name" -n "$ns"|| echo "$name not found"
    k logs "$name" -n "$ns" || echo "logs not found for $name"
    exit $?
  fi
}

trap cleanup EXIT

if k get ns/cni-plugin-test >/dev/null 2>&1 ; then
  echo 'ns/cni-plugin-test already exists' >&2
  exit 1
fi

create_test_lab

# If installing Calico, need to wait for it to roll first, otherwise pods will
# be blocked (e.g pods for linkerd-cni)
if [ "$SCENARIO" != "calico" ]; then
  wait_rollout "deploy/calico-kube-controllers" "kube-system" "2m"
  wait_rollout "daemonset/calico-node" "kube-system" "2m"

fi

# Wait for linkerd-cni daemonset to complete
wait_rollout "daemonset/linkerd-cni" "linkerd-cni" "30s"

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
    /usr/lib/linkerd/linkerd2-network-validator --log-format plain \
    --log-level debug --connect-addr 1.1.1.1:20001 \
    --listen-addr 0.0.0.0:4140 --timeout 5m

echo 'PASS: Network Validator'

calico_overrides="{
               \"apiVersion\": \"v1\",
               \"spec\": {
                  \"containers\": [
                     {
                        \"name\": \"linkerd-proxy\",
                        \"image\": \"test.l5d.io/linkerd/cni-plugin-tester:test\",
                        \"command\": [\"go\", \"test\", \"-v\", \"./cni-plugin/integration/tests/${SCENARIO}/...\"],
                        \"volumeMounts\": [
                           {
                              \"mountPath\": \"/host/etc/cni/net.d\",
                              \"name\": \"cni-net-dir\"
                           }
                        ]
                     }
                  ],
                  \"volumes\": [
                     {
                        \"name\": \"cni-net-dir\",
                        \"hostPath\": {
                           \"path\": \"/etc/cni/net.d\"
                        }
                     }
                  ]
               },
               \"status\": {}
            }"
flannel_overrides="{
               \"apiVersion\": \"v1\",
               \"spec\": {
                  \"containers\": [
                     {
                        \"name\": \"linkerd-proxy\",
                        \"image\": \"test.l5d.io/linkerd/cni-plugin-tester:test\",
                        \"command\": [\"go\", \"test\", \"-v\", \"./cni-plugin/integration/tests/${SCENARIO}/...\"],
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
            }"
# This needs to use the name linkerd-proxy so that linkerd-cni will run.
echo '# Running tester...'
if [ "$SCENARIO" == "calico" ]; then
  k run linkerd-proxy \
          --attach \
          --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
          --image-pull-policy=Never \
          --namespace=cni-plugin-test \
          --restart=Never \
          --overrides="$calico_overrides" \
          --rm
else
  k run linkerd-proxy \
          --attach \
          --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
          --image-pull-policy=Never \
          --namespace=cni-plugin-test \
          --restart=Never \
          --overrides="$flannel_overrides" \
          --rm
fi
