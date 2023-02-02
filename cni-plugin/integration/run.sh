#!/usr/bin/env bash

set -euo pipefail

cd "${BASH_SOURCE[0]%/*}"
_CALICO_YAML="https://k3d.io/v5.3.0/usage/advanced/calico.yaml"

# Help summary for test harness
_print_summary() {
  local name="${0##*/}"
  printf "Run integration tests for Linkerd's CNI plugin\n\nUsage:\n %2s ${name} --scenario [calico | flannel]\n\n"
  printf "Examples:\n"
  printf "%2s#Run integration tests using flannel as base CNI\n%2s${name} --scenario flannel\n\n"
  printf "%2s#Run integration tests using calico as base CNI\n%2s${name} --scenario calico\n\n"
}

# Make CLI opts, exit if scenario not specified
# TODO (matei): add a 'skip-cleanup' flag?
_mk_opts() {
  export SCENARIO=''

  if [ $# -eq 0 ]; then
    _print_summary "$0"
    exit 0
  fi

  while [ $# -ne 0 ]; do
    case $1 in
      -h|--help)
        _print_summary "$0"
        exit 0
        ;;
      -s|--scenario)
        SCENARIO="$2"
        if [ -z "$SCENARIO" ]; then
          echo "Error: argument for --scenario not specified" >&2
          exit 64
        fi
        shift
        shift
        ;;
      *)
    esac
  done
}

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
    if [ "$SCENARIO" == "calico" ]; then
      k create -f manifests/linkerd-cni-default.yaml 
    else
      k create -f manifests/linkerd-cni.yaml
    fi
}

function cleanup() {
    echo '# Cleaning up...'
    k delete -f manifests/linkerd-cni.yaml || echo "could not delete -f manifests/linkerd-cni.yaml"
    k delete serviceaccount linkerd-cni || echo "could not delete serviceaccount linkerd-cni"
    k delete ns cni-plugin-test || echo "could not delete namespace cni-plugin-test"

    if [ "$SCENARIO" == "calico" ]; then
       k delete -f "$_CALICO_YAML" || echo "could not delete Calico resources"
    fi
}

function install_calico() {
  echo '# Installing Calico...'
  k apply -f "$_CALICO_YAML" || echo "could not apply $_CALICO_YAML"
}

_mk_opts "$@"

trap cleanup EXIT

if k get ns/cni-plugin-test >/dev/null 2>&1 ; then
  echo 'ns/cni-plugin-test already exists' >&2
  exit 1
fi

if [ "$SCENARIO" == "calico" ]; then
  install_calico
fi
create_test_lab

# Wait for linkerd-cni daemonset to complete
if ! k rollout status --timeout=30s daemonset/linkerd-cni -n linkerd-cni; then
  echo "!! linkerd-cni didn't rollout properly, printing logs";
  k describe ds linkerd-cni || echo "daemonset linkerd-cni not found"
  k logs linkerd-cni -n linkerd-cni || echo "logs not found for linkerd-cni"
  exit $?
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
    /usr/lib/linkerd/linkerd2-network-validator --log-format plain \
    --log-level debug --connect-addr 1.1.1.1:20001 \
    --listen-addr 0.0.0.0:4140 --timeout 10s

echo 'PASS: Network Validator'

calico_overrides="{
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
            }"
# This needs to use the name linkerd-proxy so that linkerd-cni will run.
echo '# Running tester...'
if [ "$SCENARIO" == "calico"]; then
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
