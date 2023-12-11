#!/usr/bin/env bash

set -euo pipefail

# shellcheck disable=SC2086
function step() {
  repeat=$(seq 1 ${#1})
  printf "%0.s#" $repeat
  printf "#####\n# %s...\n" "$1"
  printf "%0.s#" $repeat
  printf "#####\n"
}

if [[ ! "$1" =~ (.*):(.*) ]]; then
    echo 'Usage: run.sh name:tag'
    exit 1
fi
cni_plugin_image=${BASH_REMATCH[1]}
cni_image_version=${BASH_REMATCH[2]}

cd "${BASH_SOURCE[0]%/*}"

step 'Installing Calico'
kubectl apply -f https://k3d.io/v5.1.0/usage/advanced/calico.yaml
kubectl	--namespace=kube-system wait --for=condition=available --timeout=120s \
  deploy/calico-kube-controllers

step 'Installing latest linkerd edge'
scurl https://run.linkerd.io/install-edge | sh
export PATH=$PATH:$HOME/.linkerd2/bin
linkerd install --crds | kubectl apply -f -
# The linkerd-cni-config.yml config adds an extra initContainer that will make
# linkerd-cni to delay its start for 15s, so to allow time for the pause
# DaemonSet to start before the full CNI config is ready and enter a failure
# mode
linkerd install-cni \
  --use-wait-flag \
  --cni-image "$cni_plugin_image" \
  --cni-image-version "$cni_image_version" \
  --set reinitializePods.image.name="$cni_plugin_image" \
  --set reinitializePods.image.version="$cni_image_version" \
  -f linkerd-cni-config.yml \
  | kubectl apply -f -
linkerd check --pre --linkerd-cni-enabled
linkerd install --linkerd-cni-enabled | kubectl apply -f -
linkerd check

step 'Installing pause DaemonSet'
kubectl apply -f pause-ds.yml
kubectl wait --for=condition=ready --timeout=120s -l app=pause-app po

step 'Adding a node'
cluster=$(just-k3d --evaluate K3D_CLUSTER_NAME)
image=$(just --evaluate cni-plugin-image)
k3d node create node2 --cluster "$cluster"
k3d image import --cluster "$cluster" "$image"

step 'Checking new DS replica fails with code 95'
sleep 10
kubectl wait \
  --for=jsonpath='{.status.initContainerStatuses[0].lastState.terminated.exitCode}'=95 \
  --field-selector=spec.nodeName=k3d-node2-0 \
  pod

step 'Checking new DS replica gets replaced'
for _ in {1..5}; do
  if kubectl wait --for=condition=ready --timeout=10s -l app=pause-app po; then
    break
  fi
done
kubectl wait --for=condition=ready --timeout=10s -l app=pause-app po;
