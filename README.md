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
# Build and push to custom repo
Start by building and tagging the `proxy-init` image   
For custom tagging set the TAG parameter. (default is latest)     
To build an image 
```bash
make TAG=git-1234 image
```
For building and pushing to a private registry set the DOCKER_REGISTRY parameter
```bash
make DOCKER_REGISTRY=ghcr.io/my-private-registry  TAG=git-1234 docker-push
```

To make use of the custom init-proxy you can tell linkerd during install which image to use  
Example: 
```bash
linkerd install --init-image=$DOCKER_REGISTRY:proxy-init  --init-image-version=$TAG |  kubectl apply -f - 
```

# Build Multi-Architecture Docker Images with Buildx

Please refer to [Docker Docs](https://docs.docker.com/buildx/working-with-buildx) to enable Buildx.

Run `make images` to start build the images.

Run `make push` to push the images into registry.
Registry repo can be configured with environment variable:

```bash
DOCKER_REGISTRY=<your registry> make push
```

In some local environments like Ubuntu, where the default Buildx builder uses the `docker` driver, the `make images` command might fail with the following error:

```bash
$ make images
multiple platforms feature is currently not supported for docker driver. Please switch to a different driver (eg. "docker buildx create --use")
Makefile:57: recipe for target 'images' failed
make: *** [images] Error 1
```

To fix this, you can create a new Buildx builder instance by running `make builder`. This command will create a builder that uses the `docker-container` driver that can build multi-platform images. For more information, see the Buildx builder [documentation](https://docs.docker.com/buildx/working-with-buildx/#work-with-builder-instances).
