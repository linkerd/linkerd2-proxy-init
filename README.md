# linkerd2-proxy-init

This repo contains the init container that reroutes all traffic to the pod
through Linkerd2's sidecar proxy. This rerouting is done via iptables and
requires the NET_ADMIN capability.

## Integration tests

Assuming that you have `k3d` installed locally (as is included in the
Devcontainer):

```bash
just test-integration
```
