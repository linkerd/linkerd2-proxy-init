#!/usr/bin/env bash
# Copyright (c) 2018 Tigera, Inc. All rights reserved.
# Copyright 2018 Istio Authors
# Modifications copyright (c) Linkerd authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This file was inspired by:
# 1) https://github.com/projectcalico/cni-plugin/blob/c1175467c227c1656577c80bfc0ee7795da2e2bc/k8s-install/scripts/install-cni.sh
# 2) https://github.com/istio/cni/blob/c63a509539b5ed165a6617548c31b686f13c2133/deployments/kubernetes/install/scripts/install-cni.sh

# Script to install Linkerd CNI on a Kubernetes host.
# - Expects the host CNI binary path to be mounted at /host/opt/cni/bin
# - Expects the host CNI network config path to be mounted at /host/etc/cni/net.d
# - Expects the desired CNI config in the CNI_NETWORK_CONFIG env variable

# Ensure all variables are defined, and that the script fails when an error is
# hit.
set -u -e -o pipefail +o noclobber

# Helper function for raising errors
# Usage:
# some_command || exit_with_error "some_command_failed: maybe try..."
exit_with_error() {
  log "${1}"
  exit 1
}

# The directory on the host where existing CNI plugin configs are installed
# and where this script will write out its configuration through the container
# mount point. Defaults to /etc/cni/net.d, but can be overridden by setting
# DEST_CNI_NET_DIR.
DEST_CNI_NET_DIR=${DEST_CNI_NET_DIR:-/etc/cni/net.d}
# The directory on the host where existing CNI binaries are installed. Defaults to
# /opt/cni/bin, but can be overridden by setting DEST_CNI_BIN_DIR. The linkerd-cni
# binary will end up in this directory from the host's point of view.
DEST_CNI_BIN_DIR=${DEST_CNI_BIN_DIR:-/opt/cni/bin}
# The mount prefix of the host machine from the container's point of view.
# Defaults to /host, but can be overridden by setting CONTAINER_MOUNT_PREFIX.
CONTAINER_MOUNT_PREFIX=${CONTAINER_MOUNT_PREFIX:-/host}
# The location in the container where the linkerd-cni binary resides. Can be
# overridden by setting CONTAINER_CNI_BIN_DIR. The binary in this directory
# will be copied over to the host DEST_CNI_BIN_DIR through the mount point.
CONTAINER_CNI_BIN_DIR=${CONTAINER_CNI_BIN_DIR:-/opt/cni/bin}
# Directory path where CNI configuration should live on the host
HOST_CNI_NET="${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}"
# Location of legacy "interface mode" file, to be automatically deleted
DEFAULT_CNI_CONF_PATH="${HOST_CNI_NET}/01-linkerd-cni.conf"
KUBECONFIG_FILE_NAME=${KUBECONFIG_FILE_NAME:-ZZZ-linkerd-cni-kubeconfig}
SERVICEACCOUNT_PATH=/var/run/secrets/kubernetes.io/serviceaccount

############################
### Function definitions ###
############################

# Cleanup will remove any installed configuration from the host If there are any
# *conflist files, then linkerd-cni configuration parameters will be removed
# from them.
cleanup() {
  # First, kill both 'inotifywait' processes so we don't process any
  # DELETE/CREATE events.
  pids=$(pgrep inotifywait)
  if [ -n "${pids}" ]; then
    while read -r pid; do
      log "Sending SIGKILL to inotifywait (PID: ${pid})"
      kill -s KILL "${pid}"
    done <<< "${pids}"
  fi

  log 'Removing linkerd-cni artifacts.'

  # Find all conflist files and print them out using a NULL separator instead of
  # writing each file in a new line. We will subsequently read each string and
  # attempt to rm linkerd config from it using jq helper.
  local cni_data
  find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' \) -print0 |
    while read -r -d $'\0' file; do
      log "Removing linkerd-cni config from ${file}"
      cni_data=$(jq 'del( .plugins[]? | select( .type == "linkerd-cni" ))' "${file}")
      # TODO (matei): we should write this out to a temp file and then do a `mv`
      # to be atomic. 
      echo "${cni_data}" > "${file}"
    done

  # Remove binary and kubeconfig file
  if [ -e "${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}" ]; then
    log "Removing linkerd-cni kubeconfig: ${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}"
    rm -f "${HOST_CNI_NET}/${KUBECONFIG_FILE_NAME}"
  fi
  if [ -e "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}/linkerd-cni" ]; then
    log "Removing linkerd-cni binary: ${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}/linkerd-cni"
    rm -f "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}/linkerd-cni"
  fi

  log 'Exiting.'
}

# Capture the usual signals and exit from the script
trap 'log "SIGINT received, simply exiting..."; cleanup' INT
trap 'log "SIGTERM received, simply exiting..."; cleanup' TERM
trap 'log "SIGHUP received, simply exiting..."; cleanup' HUP
trap 'log "ERROR caught, exiting..."; cleanup ' ERR

# Copy the linkerd-cni binary to a known location where CNI will look.
install_cni_bin() {
  # Place the new binaries if the mounted directory is writeable.
  dir="${CONTAINER_MOUNT_PREFIX}${DEST_CNI_BIN_DIR}"
  if [ ! -w "${dir}" ]; then
    exit_with_error "${dir} is non-writeable, failure"
  fi
  for path in "${CONTAINER_CNI_BIN_DIR}"/*; do
    cp "${path}" "${dir}/" || exit_with_error "Failed to copy ${path} to ${dir}."
  done

  log "Wrote linkerd CNI binaries to ${dir}"
}

create_kubeconfig() {
  KUBE_CA_FILE=${KUBE_CA_FILE:-${SERVICEACCOUNT_PATH}/ca.crt}
  SKIP_TLS_VERIFY=${SKIP_TLS_VERIFY:-false}
  SERVICEACCOUNT_TOKEN=$(cat "${SERVICEACCOUNT_PATH}/token")

  # Check if we're not running as a k8s pod.
  if [[ ! -f "${SERVICEACCOUNT_PATH}/token" ]]; then
    return
  fi

  if [ -z "${KUBERNETES_SERVICE_HOST}" ]; then
    log 'KUBERNETES_SERVICE_HOST not set'; exit 1;
  fi
  if [ -z "${KUBERNETES_SERVICE_PORT}" ]; then
    log 'KUBERNETES_SERVICE_PORT not set'; exit 1;
  fi

  if [ "${SKIP_TLS_VERIFY}" = 'true' ]; then
    TLS_CFG='insecure-skip-tls-verify: true'
  elif [ -f "${KUBE_CA_FILE}" ]; then
    TLS_CFG="certificate-authority-data: $(base64 "${KUBE_CA_FILE}" | tr -d '\n')"
  fi

  touch "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"
  chmod "${KUBECONFIG_MODE:-600}" "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"
  cat > "${CONTAINER_MOUNT_PREFIX}${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}" <<EOF
# Kubeconfig file for linkerd CNI plugin.
apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: ${KUBERNETES_SERVICE_PROTOCOL:-https}://${KUBERNETES_SERVICE_HOST}:${KUBERNETES_SERVICE_PORT}
    ${TLS_CFG}
users:
- name: linkerd-cni
  user:
    token: ${SERVICEACCOUNT_TOKEN}
contexts:
- name: linkerd-cni-context
  context:
    cluster: local
    user: linkerd-cni
current-context: linkerd-cni-context
EOF
}

create_cni_conf() {
  # Create temp configuration and kubeconfig files
  #
  TMP_CONF='/tmp/linkerd-cni.conf.default'
  # If specified, overwrite the network configuration file.
  CNI_NETWORK_CONFIG_FILE="${CNI_NETWORK_CONFIG_FILE:-}"
  CNI_NETWORK_CONFIG="${CNI_NETWORK_CONFIG:-}"

  # If the CNI Network Config has been overwritten, then use template from file
  if [ -e "${CNI_NETWORK_CONFIG_FILE}" ]; then
    log "Using CNI config template from ${CNI_NETWORK_CONFIG_FILE}."
    cp "${CNI_NETWORK_CONFIG_FILE}" "${TMP_CONF}"
  elif [ "${CNI_NETWORK_CONFIG}" ]; then
    log 'Using CNI config template from CNI_NETWORK_CONFIG environment variable.'
    cat <<EOF > "${TMP_CONF}"
${CNI_NETWORK_CONFIG}
EOF
  fi

  # Use alternative command character "~", since these include a "/".
  sed -i s~__KUBECONFIG_FILEPATH__~"${DEST_CNI_NET_DIR}/${KUBECONFIG_FILE_NAME}"~g "${TMP_CONF}"

  log "CNI config: $(cat "${TMP_CONF}")"
}

install_cni_conf() {
  local cni_conf_path=${1}

  # Add the linkerd-cni plugin to the existing list.
  local tmp_data
  local conf_data
  tmp_data=$(cat "${TMP_CONF}")
  conf_data=$(jq --argjson CNI_TMP_CONF_DATA "${tmp_data}" -f /linkerd/filter.jq "${cni_conf_path}" || true)

  # Ensure that CNI config file did not disappear during processing.
  [ -n "${conf_data}" ] || return 0

  echo "${conf_data}" > "${TMP_CONF}"

  # If the old config filename ends with .conf, rename it to .conflist because
  # it has changed to be a list.
  local filename
  local extension
  filename=${cni_conf_path##*/}
  extension=${filename##*.}
  # When this variable has a file, we must delete it later.
  old_file_path=
  if [ "${filename}" != '01-linkerd-cni.conf' ] && [ "${extension}" = 'conf' ]; then
    old_file_path=${cni_conf_path}
    log "Renaming ${cni_conf_path} extension to .conflist"
    cni_conf_path=${cni_conf_path}list
  fi

  # Store SHA of each patched file in global `CNI_CONF_SHA` variable.
  #
  # This must happen in a non-concurrent access context!
  #
  # The below logic assumes that the `CNI_CONF_SHA` variable is already a
  # valid JSON object. So this variable must be initialized with '{}'!
  #
  # E.g. (pretty-printed; actual variable stores compact JSON object)
  #
  # {
  #   "/etc/cni/net.d/05-foo.conflist": "b5bb9d8014a0f9b1d61e21e796d78dccdf1352f23cd32812f4850b878ae4944c",
  #   "/etc/cni/net.d/10-bar.conflist": "7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730"
  # }
  local new_sha
  new_sha=$( (sha256sum "${TMP_CONF}" || true) | awk '{print $1}' )
  CNI_CONF_SHA=$(jq -c --arg f "${cni_conf_path}" --arg sha "${new_sha}" '. * {$f: $sha}' <<< "${CNI_CONF_SHA}")

  # Move the temporary CNI config into place.
  mv "${TMP_CONF}" "${cni_conf_path}" || exit_with_error 'Failed to mv files.'
  [ -n "${old_file_path}" ] && rm -f "${old_file_path}" && log "Removing unwanted .conf file"

  log "Created CNI config ${cni_conf_path}"
}

# `sync()` is responsible for reacting to file system changes. It is used in
# conjunction with inotify events; `sync()` is called with the event type (which
# can be either 'CREATE', 'MOVED_TO', or 'MODIFY') and the name of the file that
# has changed.
#
# Based on the changed file, `sync()` might re-install the CNI configuration
# file.
sync() {
  local ev=${1}
  local file=${2//\/\//\/} # replace "//" with "/"

  [[ "${file}" =~ .*.(conflist|conf)$ ]] || return 0

  log "Detected event: ${ev} ${file}"

  # Retrieve previous SHA of detected file (if any) and compute current SHA.
  local previous_sha
  local current_sha
  previous_sha=$(jq -r --arg f "${file}" '.[$f] | select(.)' <<< "${CNI_CONF_SHA}")
  current_sha=$( (sha256sum "${file}" || true) | awk '{print $1}' )

  # If the SHA hasn't changed or the detected file has disappeared, ignore it.
  # When the SHA is the same, we can get into infinite loops whereby a file
  # has been created and after re-install the watch keeps triggering MOVED_TO
  # events that never end.
  # If the `current_sha` variable is blank then the detected CNI config file has
  # disappeared and no further action is required.
  # There exists an unhandled (highly improbable) edge case where a CNI plugin
  # creates a config file and then _immediately_ removes it again _while_ we are
  # in the process of patching it. If this happens, we may create a patched CNI
  # config file that should *not* exist.
  if [ -n "${current_sha}" ] && [ "${current_sha}" != "${previous_sha}" ]; then
    log "New/changed file [${file}] detected; re-installing"
    create_kubeconfig
    create_cni_conf
    install_cni_conf "${file}"
  else
    log "Ignoring event: ${ev} ${file}; no real changes detected or file disappeared"
  fi
}

# monitor_cni_config starts a watch on the host's CNI config directory
monitor_cni_config() {
  inotifywait -m "${HOST_CNI_NET}" -e create,moved_to,modify |
    while read -r directory action filename; do
      sync "${action}" "${directory}/${filename}"
    done
}

# This function detects whether the service account token was rotated by
# listening to MOVED_TO events under the directory
# /var/run/secrets/kubernetes.io/serviceaccount, detecting whether the ..data
# directory was moved to, as recommended by k8s' atomic writer:
# > Consumers of the target directory can monitor the ..data symlink using
# > inotify or fanotify to receive events when the content in the volume is
# > updated.
# Indeed, as per atomic writer's Write function docs, in the final steps the
# ..data_tmp symlink points to a new timestamped directory containing the new
# files, which is then atomically renamed to ..data:
# >  8. A symlink to the new timestamped directory ..data_tmp is created that
# >     will become the new data directory.
# >  9. The new data directory symlink is renamed to the data directory; rename
# >     is atomic.
# See https://github.com/kubernetes/kubernetes/blob/release-1.32/pkg/volume/util/atomic_writer.go
monitor_service_account_token() {
  inotifywait -m "${SERVICEACCOUNT_PATH}" -e moved_to |
    while read -r _ _ filename; do
      if [[ "${filename}" == "..data" ]]; then
        log "Detected change in service account files; recreating kubeconfig file"
        create_kubeconfig
      fi
    done
}

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "${1}"
}

################################
### CNI Plugin Install Logic ###
################################

# Delete old "interface mode" file, possibly left over from previous versions
# TODO(alpeb): remove this on stable-2.15
rm -f "${DEFAULT_CNI_CONF_PATH}"

install_cni_bin

# The CNI config monitor must be set up _before_ we start patching existing CNI
# config files!
# Otherwise, new CNI config files can be created just _after_ the initial round
# of patching and just _before_ we set up the `inotifywait` loop to detect new
# CNI config files.
CNI_CONF_SHA='{}'
monitor_cni_config &
monitor_pid=$!

# The following logic waits (indefinitely if need be) for `inotifywait` in the
# forked `monitor_cni_config()` function to enter "interruptible sleep" state.
# This state indicates that `inotifywait` is fully up and ready to respond to
# filesystem events.
log "Wait for CNI config monitor to become ready"
while true; do
  monitor_state=$(
    (ps --ppid=$monitor_pid -o comm=,state= || true) |
    awk '$1 == "inotifywait" && $2 == "S" {print "ok"}'
  )
  [ -z "$monitor_state" ] || break
  sleep .1 # 100ms
done

# Append our config to any existing config file (*.conflist or *.conf)
config_files=$(find "${HOST_CNI_NET}" -maxdepth 1 -type f ! -name '*linkerd*' \( -iname '*conflist' -o -iname '*conf' \))
if [ -z "${config_files}" ]; then
  log "No active CNI configuration files found"
else
  find "${HOST_CNI_NET}" -maxdepth 1 -type f \( -iname '*conflist' -o -iname '*conf' \) -print0 |
    while read -r -d $'\0' file; do
      log "Trigger CNI config detection for ${file}"
      tmp_file="$(mktemp -u /tmp/linkerd-cni.patch-candidate.XXXXXX)"
      cp -fp "${file}" "${tmp_file}"
      # The following will trigger the `sync()` function via filesystem event.
      # This requires `monitor_cni_config()` to be up and running!
      mv "${tmp_file}" "${file}" || exit_with_error 'Failed to mv files.'
    done
fi

# Watch in bg so we can receive interrupt signals through 'trap'. From 'man
# bash': 
# "If  bash  is  waiting  for a command to complete and receives a signal
# for which a trap has been set, the trap will not be executed until the command
# completes. When bash is waiting for an asynchronous command via the wait
# builtin, the reception of a signal for which a trap has been set will cause
# the wait builtin to return immediately with an exit status greater than 128,
# immediately after which the trap is executed."
monitor_service_account_token &
# uses -n so that we exit when the first background job exits (when there's an
# error)
wait -n
