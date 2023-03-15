#!/usr/bin/env bash

set -euxo pipefail

cd "${BASH_SOURCE[0]%/*}"

# Integration tests to run. Scenario is passed in as an environment variable.
# Default is 'flannel'
SCENARIO=${CNI_TEST_SCENARIO:-flannel}

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

function cleanup() {
    echo '# Cleaning up...'
    k delete -f "manifests/$SCENARIO/linkerd-cni.yaml" || echo "could not delete -f manifests/linkerd-cni.yaml"
    k delete serviceaccount linkerd-cni || echo "could not delete serviceaccount linkerd-cni"
    k delete ns cni-plugin-test || echo "could not delete namespace cni-plugin-test"

    # Collect other files that are not related to linkerd-cni and clean them up.
    # This may include CNI config files or install manifests
    for f in ./manifests/"$SCENARIO"/*.yaml
    do
      case $f in
        */linkerd-cni.yaml) true;; # ignore if linkerd-cni since it has already been deleted
        *) k delete -f "$f" || echo "could not delete test resource '$f'";;
      esac
    done
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
if [ "$SCENARIO" == "calico" ]; then
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
    --listen-addr 0.0.0.0:4140 --timeout 10s

echo 'PASS: Network Validator'

generic_config_mount="{
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
flannel_config_mount="{
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
if [ "$SCENARIO" == "calico" ] || [ "$SCENARIO" == "cilium" ]; then
  k run linkerd-proxy \
          --attach \
          --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
          --image-pull-policy=Never \
          --namespace=cni-plugin-test \
          --restart=Never \
          --overrides="$generic_config_mount" \
          --rm
else
  k run linkerd-proxy \
          --attach \
          --image="test.l5d.io/linkerd/cni-plugin-tester:test" \
          --image-pull-policy=Never \
          --namespace=cni-plugin-test \
          --restart=Never \
          --overrides="$flannel_config_mount" \
          --rm
fi
