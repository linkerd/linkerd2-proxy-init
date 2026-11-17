<!-- markdownlint-disable-file MD041 -->
<!-- Appended to PROMPT.md under "## Repository extension (supplied)". -->

## Critical paths in this repo

- **iptables.** `proxy-init` runs the `iptables` binaries from the Alpine
  runtime image.
- **CNI.** `github.com/containernetworking/cni` and the `cni-plugin` install
  and config handling.
- **Kubernetes clients.** `k8s.io/*`, `kube`, and `k8s-openapi`.
- **TLS.** `rustls` and anything in its tree.
- **Build and test base.** The Go and Rust toolchains, and `ghcr.io/linkerd/dev`.
