#!/usr/bin/env bash
set -euo pipefail

# setup.sh - Setup a calico cluster as per the instructions:
# https://k3d.io/v5.8.3/usage/advanced/calico/#1-create-the-cluster-without-flannel
#
# 1. ensure the kube config current context is set
# 2. install the calico tigera operator + poll/wait for deployment
# 3. install calico CRDs + poll/wait for availability
# 4. poll/wait for calico kube controllers and node rollout
printf '# calico/setup.sh\n'

kube_ctx=$(kubectl config current-context 2>/dev/null)
if [ -z "${kube_ctx}" ];
then
    printf "${BASH_SOURCE[0]} requires current context be set\n"
    exit 1
fi

kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.31.0/manifests/tigera-operator.yaml
kubectl --namespace tigera-operator rollout status --timeout=5m deploy/tigera-operator

until kubectl apply -f https://raw.githubusercontent.com/projectcalico/calico/v3.31.0/manifests/custom-resources.yaml 1>/dev/null 2>&1; do
    echo "retrying calico custom resources installation..."
    sleep 2
done
for resource in apiserver calico goldmane ippools whisker; do
    until kubectl wait --for=condition=available --timeout=120s tigerastatus "$resource" 1>/dev/null 2>&1; do
        echo "retrying tigerastatus/$resource availability check..."
        sleep 2
    done
done
for rollout in deployment/calico-kube-controllers daemonset/calico-node;
do
    until kubectl rollout status --namespace calico-system --timeout=2m "${rollout}" 1>/dev/null 2>&1;
    do
        echo "retry calico-system/${rollout} rollout check..."
        sleep 2
    done
done
