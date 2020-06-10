#!/bin/bash

# define some colors to use for output
WHITE=$(tput setaf 7)
NORMAL=$(tput sgr0)
REVERSE=$(tput smso)

function get_ip_for_pod(){
    local pod_name=$1
    until kubectl get pod "$pod_name" -o jsonpath='{.status.phase}' | grep Running > /dev/null ; do sleep 1 ; done

    kubectl get pod "$pod_name" --template='{{.status.podIP}}'
}

function wait_for_k8s_job_completion(){
    local job_name=$1
    until kubectl get jobs "$job_name" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' | grep True ; do printf "." && sleep 1 ; done
}

function header(){
    local msg=$1
    printf '\n%s%s%s\n' "$REVERSE" "$msg" "$NORMAL"
}

function log(){
    local msg=$1
    printf '\n%s%s%s\n' "$WHITE" "$msg" "$NORMAL"
}


TESTER_JOB_NAME=iptables-tester
LAB_YAML_FILE=iptables/iptablestest-lab.yaml

header 'Deleting any existing objects from previous test runs...'
kubectl delete -f "$LAB_YAML_FILE"
kubectl delete  "jobs/$TESTER_JOB_NAME"

# if env var not set then build the image
if [[ -z "${SKIP_BUILD_TESTER_IMAGE}" ]]; then
  header 'Building the image used in tests...'
  docker build . -f iptables/Dockerfile-tester --tag buoyantio/iptables-tester:v1
  sleep 10
fi

header 'Creating the test lab...'
kubectl create -f "$LAB_YAML_FILE"

POD_WITH_NO_RULES_IP=$(get_ip_for_pod 'pod-with-no-rules')
log "POD_WITH_NO_RULES_IP=${POD_WITH_NO_RULES_IP}"

POD_REDIRECTS_ALL_PORTS_IP=$(get_ip_for_pod 'pod-redirects-all-ports')
log "POD_REDIRECTS_ALL_PORTS_IP=${POD_REDIRECTS_ALL_PORTS_IP}"

POD_REDIRECTS_WHITELISTED_IP=$(get_ip_for_pod 'pod-redirects-whitelisted')
log "POD_REDIRECTS_WHITELISTED_IP=${POD_REDIRECTS_WHITELISTED_IP}"

POD_DOEST_REDIRECT_BLACKLISTED_IP=$(get_ip_for_pod 'pod-doesnt-redirect-blacklisted')
log "POD_DOEST_REDIRECT_BLACKLISTED_IP=${POD_DOEST_REDIRECT_BLACKLISTED_IP}"

header 'Running tester...'
cat <<EOF | kubectl create -f -
apiVersion: batch/v1
kind: Job
metadata:
  name: ${TESTER_JOB_NAME}
spec:
  template:
    metadata:
      name: ${TESTER_JOB_NAME}
    spec:
      containers:
      - name: tester
        image: buoyantio/iptables-tester:v1
        env:
          - name: POD_REDIRECTS_ALL_PORTS_IP
            value: ${POD_REDIRECTS_ALL_PORTS_IP}
          - name: POD_WITH_NO_RULES_IP
            value: ${POD_WITH_NO_RULES_IP}
          - name: POD_REDIRECTS_WHITELISTED_IP
            value: ${POD_REDIRECTS_WHITELISTED_IP}
          - name: POD_DOEST_REDIRECT_BLACKLISTED_IP
            value: ${POD_DOEST_REDIRECT_BLACKLISTED_IP}
      restartPolicy: Never
EOF

wait_for_k8s_job_completion $TESTER_JOB_NAME

header 'Test output:'
kubectl logs "jobs/$TESTER_JOB_NAME"

# Makes this script return status 0 if the test returned status 0
kubectl logs "jobs/$TESTER_JOB_NAME" 2>&1 | grep 'status:0' > /dev/null
