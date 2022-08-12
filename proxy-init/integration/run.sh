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

# Get the IP of a test pod.
function kip(){
    local name=$1
    k wait pod "$name" --namespace=proxy-init-test \
        --for=condition=ready --timeout=1m \
        >/dev/null

    k get pod "$name" --namespace=proxy-init-test \
        --template='{{.status.podIP}}'
}

if k get ns/proxy-init-test >/dev/null 2>&1 ; then
  echo 'ns/proxy-init-test already exists' >&2
  exit 1
fi

echo '# Creating the test lab...'
k create ns proxy-init-test
k create -f iptables/iptablestest-lab.yaml

POD_WITH_NO_RULES_IP=$(kip pod-with-no-rules)
echo "POD_WITH_NO_RULES_IP=${POD_WITH_NO_RULES_IP}"

POD_WITH_EXISTING_RULES_IP=$(kip pod-with-existing-rules)
echo "POD_WITH_EXISTING_RULES_IP=${POD_WITH_EXISTING_RULES_IP}"

POD_REDIRECTS_ALL_PORTS_IP=$(kip pod-redirects-all-ports)
echo "POD_REDIRECTS_ALL_PORTS_IP=${POD_REDIRECTS_ALL_PORTS_IP}"

POD_REDIRECTS_WHITELISTED_IP=$(kip pod-redirects-whitelisted)
echo "POD_REDIRECTS_WHITELISTED_IP=${POD_REDIRECTS_WHITELISTED_IP}"

POD_DOESNT_REDIRECT_BLACKLISTED_IP=$(kip pod-doesnt-redirect-blacklisted)
echo "POD_DOESNT_REDIRECT_BLACKLISTED_IP=${POD_DOESNT_REDIRECT_BLACKLISTED_IP}"

POD_IGNORES_SUBNETS_IP=$(kip pod-ignores-subnets)
echo "POD_IGNORES_SUBNETS_IP=${POD_IGNORES_SUBNETS_IP}"

echo '# Running tester...'
k run iptables-tester \
        --attach \
        --command \
        --env=POD_IGNORES_SUBNETS_IP="${POD_IGNORES_SUBNETS_IP}" \
        --env=POD_REDIRECTS_ALL_PORTS_IP="${POD_REDIRECTS_ALL_PORTS_IP}" \
        --env=POD_REDIRECTS_WHITELISTED_IP="${POD_REDIRECTS_WHITELISTED_IP}" \
        --env=POD_DOESNT_REDIRECT_BLACKLISTED_IP="${POD_DOESNT_REDIRECT_BLACKLISTED_IP}" \
        --env=POD_WITH_EXISTING_RULES_IP="${POD_WITH_EXISTING_RULES_IP}" \
        --env=POD_WITH_NO_RULES_IP="${POD_WITH_NO_RULES_IP}" \
        --image=test.l5d.io/linkerd/iptables-tester:test \
        --image-pull-policy=Never \
        --namespace=proxy-init-test \
        --quiet \
        --restart=Never \
        --rm \
        -- \
        go test -integration-tests

k delete ns proxy-init-test
