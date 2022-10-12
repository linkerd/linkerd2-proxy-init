#!/usr/bin/env bash
set -euo pipefail

# Delete all files under the buildkit blob directory that are not referred
# to any longer in the cache manifest file
manifest_sha=$(jq -r .manifests[0].digest < "$RUNNER_TEMP/.buildx-cache/index.json")
manifest=${manifest_sha#"sha256:"}
files=("$manifest")
while IFS= read -r f; do
    files+=("$f")
done < <(jq -r '.manifests[].digest | sub("^sha256:"; "")' < "$RUNNER_TEMP/.buildx-cache/blobs/sha256/$manifest")

for file in "$RUNNER_TEMP"/.buildx-cache/blobs/sha256/*; do
    for name in "${files[@]}"; do
        if [[ "${file##*/}" == "$name" ]]; then
            rm -f "$file"
            echo "deleted: $name"
            break;
        fi
    done
done
