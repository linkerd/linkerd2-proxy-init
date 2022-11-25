# This justfile includes recipes for building and packaging the validator for
# release. This file is separated so that we can invoke cargo, etc when building
# defaults. If this logic were in the top-level justfile, then these tools would
# be invoked (possibly updating Rust, etc), for unrelated targets.
#
# Users are expected to interact with this via the top-level Justfile.

# The version name to use for packages.
version := env_var_or_default("VALIDATOR_VERSION", ```
    cd .. && cargo metadata --format-version=1 \
        | jq -r '.packages[] | select(.name == "linkerd-network-validator") | .version' \
        | head -n 1
    ```)

# The architecture name to use for packages. Either 'amd64', 'arm64', or 'arm'.
_arch := env_var_or_default("ARCH", "amd64")

# If an `_arch` is specified, then we change the default cargo `--target` to
# support cross-compilation. Otherwise, we use `rustup` to find the default.
_cargo-target := if _arch == "amd64" {
        "x86_64-unknown-linux-musl"
    } else if _arch == "arm64" {
        "aarch64-unknown-linux-musl"
    } else if _arch == "arm" {
        "armv7-unknown-linux-musleabihf"
    } else {
        `rustup show | sed -n 's/^Default host: \(.*\)/\1/p'`
    }

_target-dir := "../target" / _cargo-target / "release"
_bin := _target-dir / "linkerd-network-validator"
_package-name := "linkerd-network-validator-" + version + "-" + _arch
_package-dir := "../target/package"
_shasum := "shasum -a 256"

export RUST_BACKTRACE := env_var_or_default("RUST_BACKTRACE", "short")

# Support cross-compilation when `_arch` changes.
_strip := if _arch == "arm64" { "aarch64-linux-gnu-strip" } else if _arch == "arm" { "arm-linux-gnueabihf-strip" } else { "strip" }

_cargo := env_var_or_default("CARGO", "cargo")

# If recipe is run in github actions (and cargo-action-fmt is installed), then add a
# command suffix that formats errors
_cargo-fmt := if env_var_or_default("GITHUB_ACTIONS", "") != "true" { "" } else {
    ```
    if command -v cargo-action-fmt >/dev/null 2>&1 ; then
        echo "--message-format=json | cargo-action-fmt"
    fi
    ```
}

package: build
    @mkdir -p {{ _package-dir }}
    cp {{ _bin }} {{ _package-dir / _package-name }}
    {{ _strip }} {{ _package-dir / _package-name }}
    {{ _shasum }} {{ _package-dir / _package-name }} > {{ _package-dir / _package-name }}.shasum

build *flags:
    {{ _cargo }} fetch --locked
    {{ _cargo }} build --workspace -p linkerd-network-validator \
        --release \
        --target={{ _cargo-target }} \
        {{ flags }} \
        {{ _cargo-fmt }}
