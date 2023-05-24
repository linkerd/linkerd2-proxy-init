#!/usr/bin/env bash

set -euo pipefail

cd "${BASH_SOURCE[0]%/*}"

NODE_NAME=l5d-server-extra

kubectl config use-context "k3d-$K3D_CLUSTER_NAME"

printf '\n# Install calico and linkerd-cni...\n'
kubectl apply -f manifests/calico/calico-install.yaml
kubectl apply -f manifests/calico/linkerd-cni.yaml

printf '\n# Label node and then add node selectors to calico and linkerd-cni to run only on the current node...\n'
kubectl label node "k3d-$K3D_CLUSTER_NAME-server-0" allow-calico=true
kubectl label node "k3d-$K3D_CLUSTER_NAME-server-0" allow-linkerd-cni=true
kubectl -n kube-system patch daemonsets calico-node --type=json \
	-p='[{"op": "add", "path": "/spec/template/spec/nodeSelector", "value": {"allow-calico": "true"}}]'
kubectl -n linkerd-cni patch daemonsets linkerd-cni --type=json \
	-p='[{"op": "add", "path": "/spec/template/spec/nodeSelector", "value": {"allow-linkerd-cni": "true"}}]'
kubectl rollout status daemonset -n kube-system
kubectl rollout status daemonset -n linkerd-cni

printf '\n# Create new node...\n'
k3d node create "$NODE_NAME" -c "$K3D_CLUSTER_NAME"
k3d image import test.l5d.io/linkerd/cni-plugin:test -c "$K3D_CLUSTER_NAME"

printf '\n# Start pod; k8s should not schedule it just yet...\n'
selector="{
  \"spec\": {
    \"nodeSelector\": {
      \"kubernetes.io/hostname\": \"k3d-$NODE_NAME-0\"
    }
  }
}"
kubectl run nginx --image nginx --restart Never --overrides="$selector"
sleep 10s
status=$(kubectl get po nginx -ojson | jq -r .status.containerStatuses[0])
if [[ "$status" == "null" ]]; then
		echo "Pod not scheduled as expected"
else
		echo "Unexpected pod container status: $status"
		exit 1
fi

printf '\n# Trigger linkerd-cni; k8s should still not schedule the pod...\n'
kubectl label node "k3d-$NODE_NAME-0" allow-linkerd-cni=true
kubectl rollout status daemonset -n linkerd-cni
sleep 10s
status=$(kubectl get po nginx -ojson | jq -r .status.containerStatuses[0])
if [[ "$status" == "null" ]]; then
		echo "Pod not scheduled as expected"
else
		echo "Unexpected pod container status: $status"
		exit 1
fi

printf '\n# Trigger calico; k8s should now schedule the pod...\n'
kubectl label node "k3d-$NODE_NAME-0" allow-calico=true
kubectl rollout status daemonset -n kube-system
sleep 10s
status=$(kubectl get po nginx -ojson | jq -c .status.containerStatuses[0])
if [[ "$status" == "null" ]]; then
		echo "Pod not scheduled unexpectedly"
    exit1
elif [[ $(echo "$status" | jq .state.running) == "null" ]]; then
		echo "Unexpected pod container status: $status"
		exit 1
fi
echo 'Pod scheduled as expected.'
