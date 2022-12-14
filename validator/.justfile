# This justfile includes recipes for building and packaging the validator for
# release. This file is separated so that we can invoke cargo, etc when building
# defaults. If this logic were in the top-level justfile, then these tools would
# be invoked (possibly updating Rust, etc), for unrelated targets.
#
# Users are expected to interact with this via the top-level Justfile.

# The version name to use for packages.
version := `just-cargo crate-version linkerd-network-validator`

profile := 'debug'

# The architecture name to use for packages. Either 'amd64', 'arm64', or 'arm'.
arch := env_var_or_default("TARGETARCH", "amd64")

# If an `_arch` is specified, then we change the default cargo `--target` to
# support cross-compilation. Otherwise, we use `rustup` to find the default.
_cargo-target := if arch == "amd64" {
        "x86_64-unknown-linux-musl"
    } else if arch == "arm64" {
        "aarch64-unknown-linux-musl"
    } else if arch == "arm" {
        "armv7-unknown-linux-musleabihf"
    } else {
        `rustup show | sed -n 's/^Default host: \(.*\)/\1/p'`
    }

_target-dir := "../target" / _cargo-target / profile
_target-bin := _target-dir / "linkerd-network-validator"

_package-name := "linkerd-network-validator-" + version + "-" + arch
_package-tgz := "../target/package" / _package-name + ".tgz"
_package-dir := "../target/package" / _package-name
_package-bin := _package-dir / "linkerd-network-validator"
_package-dbg := _package-bin + ".dbg"

_cargo := 'just-cargo profile=' + profile + ' target=' + _cargo-target
_objcopy := 'llvm-objcopy-' + `just-cargo --evaluate _llvm-version`
_shasum := "shasum -a 256"

package: build
    @mkdir -p {{ _package-dir }}
    {{ _objcopy }} --only-keep-debug {{ _target-bin }} {{ _package-bin }}.dbg
    {{ _objcopy }} --strip-unneeded {{ _target-bin }} {{ _package-bin }}
    {{ _objcopy }} --add-gnu-debuglink={{ _package-dbg }} {{ _package-bin }}
    tar -C ../target/package -czf {{ _package-tgz }} {{ _package-name }}
    (cd ../target/package && {{ _shasum }} {{ _package-name }}.tgz > {{ _package-name }}.txt)
    @rm -rf {{ _package-dir }}

build *flags:
    {{ _cargo }} fetch --locked
    {{ _cargo }} build --workspace -p linkerd-network-validator {{ flags }}
