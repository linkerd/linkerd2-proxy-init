# linkerd2-proxy-init

This repo contains the init container that reroutes all traffic to the pod
through Linkerd2's sidecar proxy. This rerouting is done via iptables and
requires the NET_ADMIN capability.

## Integration tests

Both the cni-plugin and the proxy-init binary have their own integration tests
that must be triggered separately. For convenience, both of them have `just`
recipes that can be used to trigger the tests locally, or in  CI.

For the tests to be run, `k3d` needs to be installed locally (assumption is
that this true, since the project offers a Devcontainer).

```bash
# List all available recipes
just --list

# Run proxy-init integration tests
just proxy-init-test-integration

## Run cni plugin integration tests
# cni plugin contains more than one scenario:
# for example, tests may be run with calico, or
# with flannel (default on k3d).
# Each plugin requires a different k3d config.
## To run _all_ the tests 
just cni-plugin-test-integration-all

# Run a specific scenario, e.g flannel:
just cni-plugin-test-integration-flannel

# Run a test with a new scenario
# useful when developing or adding a new scenario
just \
  cni-integration-scenario="myscenario"
  cni-plugin-test-integration
```

By default, all `just` recipes prepare the relevant dependencies (e.g images,
clusters, and so on). Although it is not recommended, tests may be run
exclusively without preparing any dependencies. They can be trigger either
through `just` or by calling the test runner directly.

```bash
just \
  cni-integration-scenario="<flannel | calico>" \
  _cni-plugin-test-integration
```
