This repo contains the init container that reroutes all traffic to the pod
through Linkerd2's sidecar proxy. This rerouting is done via iptables and
requires the NET_ADMIN capability.

# Integration tests

The instructions below assume that you are using
[minikube](https://github.com/kubernetes/minikube).

Start by building and tagging the `proxy-init` image required for the test:

```bash
eval $(minikube docker-env)
make image
```

Then run the tests with:

```bash
make integration-test
```

# Build Multi-Architecture Docker Images with Buildx

Please refer to [Docker Docs](https://docs.docker.com/buildx/working-with-buildx) to enable Buildx.

Run `make builder` to create Buildx instance before starting to build the images.

Run `make images` to start build the images.

Run `make push` to push the images into registry.
Registry repo can be configured with environment variable:

```bash
DOCKER_REGISTRY=<your registry> make push
```
